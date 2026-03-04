package bittorrent

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"github.com/4zv4l/gbt/fs"
	"net"
	"net/netip"
	"sync"
	"time"
)

type Swarm struct {
	Peers       *sync.Map
	WorkQueue   chan PieceWork
	ResultQueue chan PieceResult
	Manager     *fs.PieceManager
	Bitfield    *SharedBitfield
	Handshake   Handshake
}

// addPeer adds peer to the thread safe peers map and start the peer handling
func (s *Swarm) addPeer(reply TrackerReply) {
	for _, peer := range reply.Peers {
		if _, exists := s.Peers.Load(peer); exists {
			continue
		}

		go func(p netip.AddrPort) {
			conn, err := ConnectAndHandshake(p, s.Handshake)
			if err != nil {
				s.Peers.Delete(p)
				return
			}
			s.Peers.Store(p, conn)
			defer s.Peers.Delete(p)
			s.PeerWorker(conn)
		}(peer)
	}
}

// TrackerLoop will request new peers from tracker and start a goroutine for each new peer to be ready to download
func (s *Swarm) TrackerLoop(trackerURL string) error {
	reply, err := GetPeersFromTracker(trackerURL)
	if err != nil {
		return err
	}
	s.addPeer(reply)

	ticker := time.NewTicker(time.Duration(reply.Interval) * time.Second)

	for {
		<-ticker.C
		reply, err := GetPeersFromTracker(trackerURL)
		if err != nil {
			return err
		}
		s.addPeer(reply)
	}
}

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

func (s *Swarm) PeerWorker(conn net.Conn) error {
	defer conn.Close()

	bitfieldMsg := Message{
		ID:      MsgBitfield,
		Payload: s.Bitfield.ToByte(),
	}
	bitfieldMsg.WriteMessage(conn)

	interestedMsg := Message{ID: MsgInterested}
	if err := interestedMsg.WriteMessage(conn); err != nil {
		return err
	}

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
		}
	}

	for work := range s.WorkQueue {
		// if peer doesnt have piece, we put it back in the work queue for others
		if !peerBitfield.HasPiece(work.Index) {
			s.WorkQueue <- work
			time.Sleep(50 * time.Millisecond)
			continue
		}

		pieceData, err := downloadPiece(conn, work, s.Manager)
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

func downloadPiece(conn net.Conn, work PieceWork, pm *fs.PieceManager) ([]byte, error) {
	pieceBuffer := make([]byte, work.Length)

	requested := 0 // Tracks what we have asked
	pipeline := 0

	for requested < work.Length || pipeline > 0 {

		// fire off requests until we have MaxPipeline pending, or we reach the end of the piece
		for pipeline < MaxPipeline && requested < work.Length {
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
		case MsgInterested:
			unchokeMsg := Message{ID: MsgUnchoke}
			unchokeMsg.WriteMessage(conn)
		case MsgPiece:
			if len(msg.Payload) < 8 {
				return nil, fmt.Errorf("invalid piece payload")
			}

			beginOffset := binary.BigEndian.Uint32(msg.Payload[4:8])
			blockData := msg.Payload[8:]

			copy(pieceBuffer[beginOffset:], blockData)
			pipeline--
		case MsgRequest: // seeding
			if len(msg.Payload) != 12 {
				continue
			}

			index := binary.BigEndian.Uint32(msg.Payload[0:4])
			begin := binary.BigEndian.Uint32(msg.Payload[4:8])
			length := binary.BigEndian.Uint32(msg.Payload[8:12])

			blockData, err := pm.ReadBlock(int(index), int(begin), int(length))
			if err != nil {
				continue // ignore if failed to read
			}

			replyPayload := make([]byte, 8+len(blockData))
			binary.BigEndian.PutUint32(replyPayload[0:4], index)
			binary.BigEndian.PutUint32(replyPayload[4:8], begin)
			copy(replyPayload[8:], blockData)

			replyMsg := Message{ID: MsgPiece, Payload: replyPayload}
			replyMsg.WriteMessage(conn)
		case MsgChoke:
			return nil, fmt.Errorf("peer choked us in the middle of a piece")
		}
	}

	return pieceBuffer, nil
}
