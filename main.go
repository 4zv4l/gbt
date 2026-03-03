package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/4zv4l/gbt/bittorrent"
	"github.com/4zv4l/gbt/torrent"
)

func downloadLoop(t *torrent.TorrentFile, peerID [20]byte, trackerURL string) error {
	peerReply, err := bittorrent.GetPeersFromTracker(trackerURL)
	if err != nil {
		return fmt.Errorf("couldnt get peers from tracker: %w", err)
	}
	manager := make(chan bittorrent.DownloadCtx, 256)
	for i, peer := range peerReply.Peers {
		go bittorrent.DownloadFromPeer(peer, t.InfoHash, peerID, manager)
		manager <- bittorrent.DownloadCtx{Index: i, Sha1sum: t.Pieces[i]}
	}

	ticker := time.NewTicker(time.Second * time.Duration(peerReply.Interval))
	defer ticker.Stop()

	for {
		select {
		case chunk := <-manager:
			f, err := os.OpenFile("", os.O_WRONLY, 0660)
			if err != nil {
				return err
			}
			defer f.Close()
			f.WriteAt(chunk.Bytes, int64(chunk.Index * t.PieceLength))
		case <-ticker.C:
			peerReply, err = bittorrent.GetPeersFromTracker(trackerURL)
			if err != nil {
				return fmt.Errorf("couldnt get peers from tracker: %w", err)
			}
			// TODO: better peer handling, only create new goroutine for new peers
			for _, peer := range peerReply.Peers {
				go bittorrent.DownloadFromPeer(peer, t.InfoHash, peerID, manager)
			}
		}
	}
}

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
	t, err := torrent.Parse(torrentfile)
	if err != nil {
		slog.Error("couldnt parse torrent file", "error", err)
		return
	}
	slog.Info("parsed torrent file", "infoHash", t.InfoHash)

	var peerID [20]byte
	rand.Read(peerID[:])
	slog.Info("generated peer ID", "peerID", peerID)

	trackerURL, err := t.TrackerURL(peerID)
	if err != nil {
		slog.Error("couldnt generate tracker URL", "error", err)
		return
	}
	slog.Info("generated tracker URL", "trackerURL", trackerURL)

	err = downloadLoop(t, peerID, trackerURL)
	if err != nil {
		slog.Error("failed download", "error", err)
		return
	}
}
