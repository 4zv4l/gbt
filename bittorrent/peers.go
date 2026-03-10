package bittorrent

import (
	"fmt"
	"net"
	"net/netip"
	"time"
)

const BlockSize = 16384 // 16 KB is the BitTorrent block size
var MaxPipeline int = 5 // send MaxPipeline requests (for 16kb block) at a time

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
	_, err := conn.Write(handshake.ToByte())
	if err != nil {
		return err
	}
	peerHandshake, err := ReadHandshake(conn)
	if err != nil {
		return err
	}
	if peerHandshake.InfoHash != handshake.InfoHash {
		return fmt.Errorf("HandlePeer(): infohash doesnt match")
	}
	return nil
}

func connectAndHandshake(peer netip.AddrPort, handshake Handshake) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", peer.String(), 3*time.Second)
	if err != nil {
		return nil, err
	}

	if err = handleHandshake(conn, handshake); err != nil {
		return nil, err
	}
	return conn, nil
}

// AddPeer connect to each peers, do the handshake and start their worker
func (s *Swarm) AddPeer(peers []netip.AddrPort) {
	for _, peer := range peers {
		if _, exists := s.Peers.Load(peer); exists {
			continue
		}

		go func(p netip.AddrPort) {
			conn, err := connectAndHandshake(p, s.Handshake)
			if err != nil {
				return
			}
			defer conn.Close()
			s.startWorker(conn, p)
		}(peer)
	}
}
