package downloader

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// parsePeers parses addresses like
// 127.0.0.1:6881,superhost.com:8866,10.10.10.5:9988
// into a list of AddrPort ready to be used by bittorrent.AddPeer
func ParsePeers(strPeers string) ([]netip.AddrPort, error) {
	var peers []netip.AddrPort
	if strPeers == "" {
		return []netip.AddrPort{}, nil
	}

	for _, strPeer := range strings.Split(strPeers, ",") {
		host, port, _ := net.SplitHostPort(strPeer)
		addrs, err := net.LookupHost(host)
		if err != nil {
			return nil, err
		}
		peer, err := netip.ParseAddrPort(fmt.Sprintf("%s:%s", addrs[0], port))
		if err != nil {
			return nil, err
		}
		peers = append(peers, peer)
	}
	return peers, nil
}

// FormatBytes show bytes in Human Readable Format
func FormatBytes(b int) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
