package bittorrent

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/4zv4l/gbt/fs"
	"github.com/4zv4l/gbt/torrent"
)

const BlockSize = 16384 // 16 KB is the BitTorrent block size

// Swarm is the struct that handle all the Peers and contains their context
type Swarm struct {
	Peers         *sync.Map
	WorkQueue     chan PieceWork
	ResultQueue   chan PieceResult
	Manager       *fs.PieceManager
	Bitfield      *SharedBitfield
	Handshake     Handshake
	SeededCounter *atomic.Uint64
	MaxPipeline   int
}

// startWorker add peer to the thread safe peer map
// and start the peer handling
func (s *Swarm) startWorker(conn net.Conn, peer netip.AddrPort) {
	s.Peers.Store(peer, conn)
	defer s.Peers.Delete(peer)
	s.work(conn)
}

// UpdatePeers send a "have" message to the peers
// telling them we have a new piece available if they need it
func (s *Swarm) UpdatePeers(piece PieceResult) {
	havePayload := make([]byte, 4)
	binary.BigEndian.PutUint32(havePayload, uint32(piece.Index))
	haveBytes := Message{ID: MsgHave, Payload: havePayload}.ToByte()
	s.Peers.Range(func(key, value any) bool {
		if conn, ok := value.(net.Conn); ok {
			conn.Write(haveBytes)
		}
		return true
	})
}

// check if part of the file are already on disk
// if not add the part to the WorkQueue
func (s *Swarm) WithResume(t *torrent.TorrentFile, completedPieces map[int]bool, downloadedPieces *int) *Swarm {
	for i, hash := range t.Pieces {
		length := t.PieceLength
		if i == len(t.Pieces)-1 {
			if remainder := t.TotalLength % t.PieceLength; remainder != 0 {
				length = remainder
			}
		}

		data, err := s.Manager.ReadPiece(i, length)
		if err == nil && sha1.Sum(data) == hash {
			completedPieces[i] = true
			*downloadedPieces++
			s.Bitfield.SetPiece(i)
		} else {
			s.WorkQueue <- PieceWork{
				Index:  i,
				Hash:   hash[:],
				Length: length,
			}
		}
	}
	return s
}

// Work send Bitfield and Interested Message
// then wait to be unchoked or to receive Bitfield
// it will then start downloading pieces from the WorkQueue
// or start seeding if the queue is empty
func (s *Swarm) work(conn net.Conn) error {
	Message{ID: MsgBitfield, Payload: s.Bitfield.ToByte()}.WriteMessage(conn)
	Message{ID: MsgInterested}.WriteMessage(conn)

	choked := true
	peerBitfield := Bitfield{}

	// wait until we know what pieces peer has or they unchoke us
	for choked || peerBitfield == nil {
		msg, err := ReadMessage(conn)
		if err != nil {
			return err
		}
		// keep-alive
		if msg == nil {
			continue
		}

		switch msg.ID {
		case MsgBitfield:
			peerBitfield = msg.Payload
		case MsgUnchoke:
			choked = false
		case MsgChoke:
			choked = true
		default:
			if err := s.handleCommonMsg(conn, msg); err != nil {
				return err
			}
		}
	}

	// if Queue is empty, start seeding only
	if len(s.WorkQueue) == 0 {
		for {
			msg, err := ReadMessage(conn)
			if err != nil {
				return err
			}
			if msg == nil {
				continue
			}
			s.handleCommonMsg(conn, msg)
		}
	}

	for work := range s.WorkQueue {
		// if peer doesnt have piece, we put it back in the work queue for others
		if !peerBitfield.HasPiece(work.Index) {
			s.WorkQueue <- work
			time.Sleep(50 * time.Millisecond)
			continue
		}

		pieceData, err := s.downloadPiece(conn, work)
		if err != nil {
			s.WorkQueue <- work
			return err
		}

		if sha1.Sum(pieceData) != [20]byte(work.Hash) {
			s.WorkQueue <- work
			continue
		}

		s.ResultQueue <- PieceResult{Index: work.Index, Data: pieceData}
	}
	return nil
}

func (s *Swarm) downloadPiece(conn net.Conn, work PieceWork) ([]byte, error) {
	var (
		pieceBuffer = make([]byte, work.Length)
		requested   = 0 // track what we have already asked (index)
		pipeline    = 0
	)

	for requested < work.Length || pipeline > 0 {

		// fire off requests until we have MaxPipeline pending, or we reach the end of the piece
		for pipeline < s.MaxPipeline && requested < work.Length {
			currentBlockSize := min(BlockSize, work.Length-requested)

			// Idx(4b) + StartOff(4b) + EndOff(4b)
			payload := make([]byte, 12)
			binary.BigEndian.PutUint32(payload[0:4], uint32(work.Index))
			binary.BigEndian.PutUint32(payload[4:8], uint32(requested))
			binary.BigEndian.PutUint32(payload[8:12], uint32(currentBlockSize))

			reqMsg := Message{ID: MsgRequest, Payload: payload}
			if err := reqMsg.WriteMessage(conn); err != nil {
				return nil, err
			}

			requested += currentBlockSize
			pipeline++
		}

		msg, err := ReadMessage(conn)
		if err != nil {
			return nil, err
		}
		if msg == nil {
			continue // keep-alive
		}

		switch msg.ID {
		case MsgPiece:
			if len(msg.Payload) < 8 {
				return nil, fmt.Errorf("invalid piece payload")
			}

			beginOffset := binary.BigEndian.Uint32(msg.Payload[4:8])
			blockData := msg.Payload[8:]

			copy(pieceBuffer[beginOffset:], blockData)
			pipeline--
		default:
			if err := s.handleCommonMsg(conn, msg); err != nil {
				return nil, err
			}
		}
	}

	return pieceBuffer, nil
}

// StartActiveSeeding start seeding server
// and return the port used (in case the given port is 0)
func (s *Swarm) StartActiveSeeding(port int) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to start seeder port: %w", err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		go func() {
			defer conn.Close()

			if err := handleHandshake(conn, s.Handshake); err != nil {
				return
			}

			addrPort, _ := netip.ParseAddrPort(conn.RemoteAddr().String())
			s.startWorker(conn, addrPort)
		}()
	}
}
