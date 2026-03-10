package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// gracefully handle ctrl-c
func setupGracefulShutdown(cancel context.CancelFunc) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintf(os.Stderr, "\r\033[K")
		log.Printf("Ctrl-C pressed, shutting down...")
		cancel()
	}()
}

// parsePeers parses addresses like
// 127.0.0.1:6881,superhost.com:8866,10.10.10.5:9988
// into a list of AddrPort ready to be used by bittorrent.AddPeer
func parsePeers(strPeers string) ([]netip.AddrPort, error) {
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
