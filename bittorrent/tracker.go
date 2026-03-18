package bittorrent

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/4zv4l/gbt/bencode"
)

type TrackerReply struct {
	Interval int
	Peers    []netip.AddrPort
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
	proto := strings.Split(trackerUrl, ":")[0]
	switch proto {
	case "http":
		return httpTracker(trackerUrl)
	case "udp":
		return udpTracker(trackerUrl)
	default:
		return TrackerReply{}, fmt.Errorf("%s tracker not supported", proto)
	}
}

func httpTracker(trackerUrl string) (TrackerReply, error) {
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
		Interval: reply["interval"].(int),
		Peers:    rawPeersToAddrPort([]byte(reply["peers"].(string))),
	}, nil
}

func udpTracker(trackerUrl string) (TrackerReply, error) {
	u, _ := url.Parse(trackerUrl)
	q := u.Query()
	port, _ := strconv.Atoi(q.Get("port"))
	left, _ := strconv.ParseUint(q.Get("left"), 10, 64)

	conn, err := net.DialTimeout("udp", u.Host, 5*time.Second)
	if err != nil {
		return TrackerReply{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	req := make([]byte, 98)
	resp := make([]byte, 2048)
	txID := rand.Uint32()

	// "handshake" (tracker will reply with an ID)
	binary.BigEndian.PutUint64(req[0:8], 0x41727101980) // Magic Number
	binary.BigEndian.PutUint32(req[12:16], txID)        // Action is already 0 by default

	conn.Write(req[:16])
	if _, err := conn.Read(resp); err != nil || binary.BigEndian.Uint32(resp[4:8]) != txID {
		return TrackerReply{}, fmt.Errorf("udp connect failed or timeout")
	}

	// announce
	binary.BigEndian.PutUint64(req[0:8], binary.BigEndian.Uint64(resp[8:16])) // Copy Connection ID from response
	binary.BigEndian.PutUint32(req[8:12], 1)                                  // Action: 1 (Announce)
	copy(req[16:36], q.Get("info_hash"))
	copy(req[36:56], q.Get("peer_id"))
	binary.BigEndian.PutUint64(req[64:72], left)
	binary.BigEndian.PutUint32(req[92:96], 0xFFFFFFFF) // NumWant: -1 (tracker send default amount of peers)
	binary.BigEndian.PutUint16(req[96:98], uint16(port))

	conn.Write(req)
	n, err := conn.Read(resp)
	if err != nil || n < 20 || binary.BigEndian.Uint32(resp[0:4]) != 1 {
		return TrackerReply{}, fmt.Errorf("udp announce failed")
	}

	fmt.Println(rawPeersToAddrPort(resp[20:n]))
	return TrackerReply{
		Interval: int(binary.BigEndian.Uint32(resp[8:12])),
		Peers:    rawPeersToAddrPort(resp[20:n]),
	}, nil
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
