package bittorrent

import (
	"fmt"
	"net"
	"net/netip"
	"time"
)

type Handshake struct {
	Length   uint8 // value should always be 19
	Pstr     string
	ExtFlag  [8]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

func (h Handshake) ToByte() []byte {
	var buf []byte
	buf = append(buf, byte(h.Length))
	buf = append(buf, h.Pstr[:]...)
	buf = append(buf, h.ExtFlag[:]...)
	buf = append(buf, h.InfoHash[:]...)
	buf = append(buf, h.PeerID[:]...)
	return buf
}

func HandshakeFromByte(rawHandshake []byte) Handshake {
	return Handshake{
		Length:   rawHandshake[0],
		Pstr:     string(rawHandshake[1:20]),
		ExtFlag:  [8]byte(rawHandshake[20:28]),
		InfoHash: [20]byte(rawHandshake[28:48]),
		PeerID:   [20]byte(rawHandshake[48:68]),
	}
}

func MakeHandshake(infohash, peerID [20]byte) Handshake {
	return Handshake{
		Length:   19,
		Pstr:     "BitTorrent protocol",
		InfoHash: infohash,
		PeerID:   peerID,
	}
}

func HandlePeer(peer netip.AddrPort, infohash, peerID [20]byte) (*string, error) {
	conn, err := net.DialTimeout("tcp", peer.String(), 3*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Handshake
	{
		var buf [68]byte
		_, err = conn.Write(MakeHandshake(infohash, peerID).ToByte())
		if err != nil {
			return nil, err
		}
		len, err := conn.Read(buf[:])
		if err != nil {
			return nil, err
		}
		handshake := HandshakeFromByte(buf[:len])
		if handshake.InfoHash != infohash {
			return nil, fmt.Errorf("HandlePeer(): infohash doesnt match")
		}
	}

	//downloadPieces(conn, )

	return nil, nil
}
