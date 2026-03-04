package bittorrent

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"
)

const BlockSize = 16384 // 16 KB is the BitTorrent block size
const MaxPipeline = 5   // send 5 requests (for 16kb block) at a time

type PieceWork struct {
	Index  int
	Hash   []byte
	Length int
}

type PieceResult struct {
	Index int
	Data  []byte
}

func handleHandshake(conn net.Conn, handshake Handshake) error {
	var buf [68]byte
	_, err := conn.Write(handshake.ToByte())
	if err != nil {
		return err
	}
	len, err := io.ReadFull(conn, buf[:])
	if err != nil {
		return err
	}
	if HandshakeFromByte(buf[:len]).InfoHash != handshake.InfoHash {
		return fmt.Errorf("HandlePeer(): infohash doesnt match")
	}
	return nil
}

func ConnectAndHandshake(peer netip.AddrPort, handshake Handshake) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", peer.String(), 3*time.Second)
	if err != nil {
		return nil, err
	}

	if err = handleHandshake(conn, handshake); err != nil {
		return nil, err
	}
	return conn, nil
}

func PeerWorker(conn net.Conn, workQueue chan PieceWork, resultQueue chan PieceResult) error {
	defer conn.Close()

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

	for work := range workQueue {
		// if peer doesnt have piece, we put it back in the work queue for others
		if !peerBitfield.HasPiece(work.Index) {
			workQueue <- work
			time.Sleep(50 * time.Millisecond)
			continue
		}

		pieceData, err := downloadPiece(conn, work)
		if err != nil {
			workQueue <- work
			return err
		}

		if sha1.Sum(pieceData) != [20]byte(work.Hash) {
			workQueue <- work
			continue
		}

		resultQueue <- PieceResult{Index: work.Index, Data: pieceData}
	}
	return nil
}

func downloadPiece(conn net.Conn, work PieceWork) ([]byte, error) {
	pieceBuffer := make([]byte, work.Length)

	requested := 0 // Tracks what we have ASKED for
	pipeline := 0

	for requested < work.Length || pipeline > 0 {

		// Fire off requests until we have MaxPipeline pending, or we reach the end of the piece
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

		case MsgChoke:
			return nil, fmt.Errorf("peer choked us in the middle of a piece")
		}
	}

	return pieceBuffer, nil
}
