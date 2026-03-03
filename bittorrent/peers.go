package bittorrent

import (
	"crypto/sha1"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"time"
)

// DownloadCtx is used as context when downloading chunk from a client
type DownloadCtx struct {
	Index   int
	Bytes   []byte
	Sha1sum [20]byte
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

// DownloadFromPeer will try to download a piece from a peer
func DownloadFromPeer(peer netip.AddrPort, handshake Handshake, manager chan DownloadCtx) error {
	conn, err := net.DialTimeout("tcp", peer.String(), 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err = handleHandshake(conn, handshake); err != nil {
		return err
	}

	ticker := time.NewTicker(time.Second * 1)

	for {
		select {
		case ctx := <-manager: // receive a piece to download
			bytes, err := downloadChunk(conn, ctx)
			if err != nil {
				return err
			}
			if sha1.Sum(bytes) != ctx.Sha1sum {
				return fmt.Errorf("sha1sum for piece #%d doesnt match", ctx.Index)
			}
			manager <- DownloadCtx{Index: ctx.Index, Bytes: bytes}
		case <-ticker.C:
			conn.Write([]byte{0, 0, 0, 0}) // keep-alive
		}

	}
}

// peerMsgLoop communicate with the peer until the piece(s) are fully downloaded
// or leave if the peer doesnt have or doesnt wanna share the piece(s)
func downloadChunk(conn net.Conn, ctx DownloadCtx) ([]byte, error) {
	chocked := true
	_ = chocked
	_ = ctx
	for {
		msg, err := ReadMessage(conn)
		if err != nil {
			return nil, err
		}

		if msg == nil { // keep-alive
			continue
		}

		switch msg.ID {
		case MsgChoke:
			slog.Info("received choke", "msg", *msg)
			chocked = true
		case MsgUnchoke:
			slog.Info("received unchoke", "msg", *msg)
			chocked = false
		case MsgInterested:
			slog.Info("received interested", "msg", *msg)
		case MsgNotInterested:
			slog.Info("received not interested", "msg", *msg)
		case MsgHave:
			slog.Info("received have", "msg", *msg)
		case MsgBitfield:
			slog.Info("received bitfield", "msg", *msg)
		case MsgRequest:
			slog.Info("received request", "msg", *msg)
		case MsgPiece:
			slog.Info("received piece", "msg", *msg)
		case MsgCancel:
			slog.Info("received cancel", "msg", *msg)
		}
	}
}
