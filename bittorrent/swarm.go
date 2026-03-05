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
)

// Swarm is the struct that handle all the Peers
// its what the workers have as context
type Swarm struct {
	Peers         *sync.Map
	WorkQueue     chan PieceWork
	ResultQueue   chan PieceResult
	Manager       *fs.PieceManager
	Bitfield      *SharedBitfield
	Handshake     Handshake
	SeededCounter *atomic.Uint64
}

// startWorker add peer to the thread safe peer map
// and start the peer handling
func (s *Swarm) startWorker(conn net.Conn, peer netip.AddrPort) {
	s.Peers.Store(peer, conn)
	defer s.Peers.Delete(peer)
	s.PeerWorker(conn)
}

// AddPeer connect to each peers, try the handshake and start their worker
func (s *Swarm) AddPeer(peers []netip.AddrPort) {
	for _, peer := range peers {
		if _, exists := s.Peers.Load(peer); exists {
			continue
		}

		go func(p netip.AddrPort) {
			conn, err := ConnectAndHandshake(p, s.Handshake)
			if err != nil {
				return
			}
			s.startWorker(conn, p)
		}(peer)
	}
}

// TrackerLoop will request new peers from tracker and start a goroutine for each new peer to be ready to download
func (s *Swarm) TrackerLoop(trackerURL string) error {
	reply, err := GetPeersFromTracker(trackerURL)
	if err != nil {
		return err
	}
	s.AddPeer(reply.Peers)

	ticker := time.NewTicker(time.Duration(reply.Interval) * time.Second)

	for {
		<-ticker.C
		reply, err := GetPeersFromTracker(trackerURL)
		if err != nil {
			return err
		}
		s.AddPeer(reply.Peers)
	}
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

// handleCommonMsg processes requests and state changes that are always handled the exact same way
func (s *Swarm) handleCommonMsg(conn net.Conn, msg *Message) error {
	switch msg.ID {
	case MsgNotInterested:
		return fmt.Errorf("peer is not interested")
	case MsgInterested:
		unchokeMsg := Message{ID: MsgUnchoke}
		return unchokeMsg.WriteMessage(conn)
	case MsgRequest:
		if len(msg.Payload) != 12 {
			return nil
		}
		index := binary.BigEndian.Uint32(msg.Payload[0:4])
		begin := binary.BigEndian.Uint32(msg.Payload[4:8])
		length := binary.BigEndian.Uint32(msg.Payload[8:12])

		blockData, err := s.Manager.ReadBlock(int(index), int(begin), int(length))
		if err != nil {
			return nil
		}

		replyPayload := make([]byte, 8+len(blockData))
		binary.BigEndian.PutUint32(replyPayload[0:4], index)
		binary.BigEndian.PutUint32(replyPayload[4:8], begin)
		copy(replyPayload[8:], blockData)

		if err := (Message{ID: MsgPiece, Payload: replyPayload}.WriteMessage(conn)); err != nil {
			return err
		}
		s.SeededCounter.Add(1)
	}
	return nil
}

func (s *Swarm) PeerWorker(conn net.Conn) error {
	defer conn.Close()

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

		go func(c net.Conn) {
			defer c.Close()

			if err := handleHandshake(conn, s.Handshake); err != nil {
				return
			}

			addrPort, _ := netip.ParseAddrPort(conn.RemoteAddr().String())
			s.startWorker(conn, addrPort)
		}(conn)
	}
}
