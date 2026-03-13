package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/4zv4l/gbt/downloader"
	"github.com/4zv4l/gbt/torrent"
)

// receive event/err from bittorrent.Download
func eventLoop(progressCH chan downloader.Progress, errCH chan error) error {
	start := time.Now()
	for {
		select {
		case err := <-errCH:
			fmt.Printf("\r\033[K")
			return err
		case p, isOpen := <-progressCH:
			if !isOpen {
				fmt.Printf("\r\033[K")
				fmt.Printf("Download completed in %v\n", time.Since(start))
				return nil
			}
			fmt.Printf("\rDownloaded: %d/%d pieces | Seeded: %d blocks", p.DownloadedPieces, p.TotalPieces, p.SeededBlocks)
		}
	}
}

func main() {
	var (
		filepath     = flag.String("file", "", "torrent file")
		port         = flag.Int("port", -1, "port to listen for peers (<=0 to not listen)")
		seed         = flag.Bool("seed", false, "keep running and seeding after download completes")
		directPeers  = flag.String("peer", "", "IP:Port of a direct peers (separated by ',') to connect to")
		pipelineSize = flag.Int("pipeline", 5, "pipeline size for peer request")
		ctx, cancel  = signal.NotifyContext(context.Background(), os.Interrupt)
	)
	defer cancel()

	flag.Parse()
	if *filepath == "" {
		flag.Usage()
		return
	}

	torrentfile, err := os.ReadFile(*filepath)
	if err != nil {
		log.Fatalf("couldnt read torrent file: %v", err)
	}
	t, err := torrent.Parse(torrentfile)
	if err != nil {
		log.Fatalf("couldnt parse torrent file: %v", err)
	}

	var peerID [20]byte
	rand.Read(peerID[:])

	trackerURL, err := t.TrackerURL(peerID, *port)
	if err != nil {
		log.Fatalf("couldnt generate tracker URL: %v", err)
	}

	log.Printf("=== Info for %s ===", t.Name)
	log.Printf("Announce:    %s", t.Announce)
	log.Printf("InfoHash:    %x", t.InfoHash)
	log.Printf("PieceLength: %s", downloader.FormatBytes(t.PieceLength))
	log.Printf("TotalLength: %s", downloader.FormatBytes(t.TotalLength))
	log.Printf("Piece Count: %d", len(t.Pieces))
	log.Printf("Peer ID:     %x", peerID)
	log.Printf("Files (%d):", len(t.Files))
	for i, f := range t.Files {
		log.Printf("  [%d] %s (%s)", i, f.Path, downloader.FormatBytes(f.Length))
	}

	peers, err := downloader.ParsePeers(*directPeers)
	if err != nil {
		log.Fatalf("failed to parse peers: %v", err)
	}

	progressCH, errCH := downloader.Download(ctx, t, peerID, trackerURL, *pipelineSize, *port, *seed, peers)
	if err = eventLoop(progressCH, errCH); err != nil {
		log.Fatalf("%v", err)
	}
}
