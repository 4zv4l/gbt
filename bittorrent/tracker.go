package bittorrent

import (
	"encoding/binary"
	"fmt"
	"github.com/4zv4l/gbt/bencode"
	"io"
	"net/http"
	"net/netip"
	"strings"
)

type TrackerReply struct {
	interval int
	peers    []netip.AddrPort
}

// each peer is 6 bytes (4 bytes IP + 2 bytes Port)
// only handle ipv4
func rawPeersToAddrPort(rawPeers []byte) []netip.AddrPort {
	var peers []netip.AddrPort
	for i := 0; i < len(rawPeers); i += 6 {
		ip := netip.AddrFrom4([4]byte(rawPeers[i : i+4]))
		port := binary.BigEndian.Uint16(rawPeers[i+4 : i+6])
		peers = append(peers, netip.AddrPortFrom(ip, port))
	}
	return peers
}

// Retrieving peers from the tracker
func GetPeersFromTracker(trackerUrl string) (TrackerReply, error) {
	resp, err := http.Get(trackerUrl)
	if err != nil {
		return TrackerReply{}, err
	}
	defer resp.Body.Close()

	bencoded, err := io.ReadAll(resp.Body)
	if err != nil {
		return TrackerReply{}, err
	}

	bdecoded, err := bencode.Decode(strings.NewReader(string(bencoded)))
	if err != nil {
		return TrackerReply{}, err
	}

	reply, ok := bdecoded.(map[string]any)
	if !ok {
		return TrackerReply{}, fmt.Errorf("invalid tracker reply: not a dictionary")
	}

	return TrackerReply{
		interval: reply["interval"].(int),
		peers:    rawPeersToAddrPort([]byte(reply["peers"].(string))),
	}, nil
}
