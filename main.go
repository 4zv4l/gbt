package main

import (
	"crypto/rand"
	"flag"
	"log/slog"
	"os"

	"github.com/4zv4l/gbt/bittorrent"
	"github.com/4zv4l/gbt/torrent"
)

func main() {
	var (
		filepath = flag.String("file", "", "torrent file to download")
	)

	flag.Parse()
	if *filepath == "" {
		slog.Error("-file flag is required")
		return
	}

	torrentfile, err := os.ReadFile(*filepath)
	if err != nil {
		slog.Error("couldnt read torrent file", "error", err)
		return
	}
	torrent, err := torrent.Parse(torrentfile)
	if err != nil {
		slog.Error("couldnt parse torrent file", "error", err)
		return
	}

	var peerID [20]byte
	rand.Read(peerID[:])
	slog.Info("generated peer ID", "peerID", peerID)

	trackerURL, err := torrent.TrackerURL(peerID)
	if err != nil {
		slog.Error("couldnt generate tracker URL", "error", err)
		return
	}
	slog.Info("generated tracker URL", "trackerURL", trackerURL)

	reply, err := bittorrent.GetPeersFromTracker(trackerURL)
	if err != nil {
		slog.Error("couldnt get peers from tracker", "error", err)
		return
	}
	slog.Info("tracker's reply", "reply", reply)
}
