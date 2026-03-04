package bittorrent

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"time"
)

const BlockSize = 16384 // 16 KB is the BitTorrent block size

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
	slog.Info("handshake made", "handshake", handshake)
	return nil
}

func ConnectAndHandshake(peer netip.AddrPort, handshake Handshake) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", peer.String(), 3*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

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

	// wait until we know what pieces peer has and they unchoke us
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
			slog.Error("download failed, returning to queue", "piece", work.Index)
			workQueue <- work
			return err
		}

		if sha1.Sum(pieceData) != [20]byte(work.Hash) {
			slog.Error("hash mismatch", "piece", work.Index)
			workQueue <- work
			continue
		}

		resultQueue <- PieceResult{Index: work.Index, Data: pieceData}
	}
	return nil
}

func downloadPiece(conn net.Conn, work PieceWork) ([]byte, error) {
	pieceBuffer := make([]byte, work.Length)
	downloaded := 0

	for downloaded < work.Length {
		// ask for the correct block-size
		// if the last piece is < 16kb ask for < 16kb
		currentBlockSize := min(BlockSize, work.Length-downloaded)

		// create payload, asking for piece Index
		// range downloaded:currentBlockSize
		payload := make([]byte, 12)
		binary.BigEndian.PutUint32(payload[0:4], uint32(work.Index))
		binary.BigEndian.PutUint32(payload[4:8], uint32(downloaded))
		binary.BigEndian.PutUint32(payload[8:12], uint32(currentBlockSize))

		reqMsg := Message{
			ID:      MsgRequest,
			Payload: payload,
		}
		if err := reqMsg.WriteMessage(conn); err != nil {
			return nil, err
		}

		msg, err := ReadMessage(conn)
		if err != nil {
			return nil, err
		}
		if msg == nil {
			continue // Keep-alive
		}

		switch msg.ID {
		case MsgPiece:
			// Payload is: Index (4 bytes), Begin Offset (4 bytes), Block Data (remainder)
			if len(msg.Payload) < 8 {
				return nil, fmt.Errorf("invalid piece payload")
			}

			// extract begin offset and data
			beginOffset := binary.BigEndian.Uint32(msg.Payload[4:8])
			blockData := msg.Payload[8:]

			// copy and increment our downloaded counter
			copy(pieceBuffer[beginOffset:], blockData)
			downloaded += len(blockData)

		case MsgChoke:
			return nil, fmt.Errorf("peer choked us")
		}
	}

	return pieceBuffer, nil
}
