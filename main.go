package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/4zv4l/gbt/bittorrent"
	"github.com/4zv4l/gbt/fs"
	"github.com/4zv4l/gbt/torrent"
)

func download(t *torrent.TorrentFile, peerID [20]byte, trackerURL string, port int, seed bool, directPeers []netip.AddrPort) error {
	var (
		start            = time.Now()
		totalPieces      = len(t.Pieces)
		downloadedPieces = 0
		completedPieces  = map[int]bool{}
		seededBlocks     atomic.Uint64
		workQueue        = make(chan bittorrent.PieceWork, totalPieces)
		resultQueue      = make(chan bittorrent.PieceResult, totalPieces)
		myBitfield       = bittorrent.NewSharedBitfield(totalPieces)
		ctx, cancel      = context.WithCancel(context.Background())
		seedTicker       = time.NewTicker(500 * time.Millisecond)
	)
	defer cancel()
	// tick every 500ms to update download/upload status
	defer seedTicker.Stop()

	setupGracefulShutdown(cancel)

	// prepare files (pre-create)
	pm, err := fs.NewPieceManager(t.Files, t.PieceLength)
	if err != nil {
		return err
	}
	defer pm.Close()

	// setup swamp that handles the workers
	// and resume download if part of the file are already on disk
	swarm := bittorrent.Swarm{
		Peers:         &sync.Map{},
		WorkQueue:     workQueue,
		ResultQueue:   resultQueue,
		Manager:       pm,
		Handshake:     bittorrent.MakeHandshake(t.InfoHash, peerID),
		Bitfield:      myBitfield,
		SeededCounter: &seededBlocks,
	}.WithResume(t, completedPieces, &downloadedPieces)

	// start actively listening
	if port > 0 {
		go swarm.StartActiveSeeding(port)
	}

	// getting peers, local through cli or through tracker
	swarm.AddPeer(directPeers)
	go swarm.TrackerLoop(trackerURL)

	// receive and write pieces
	for seed || downloadedPieces < totalPieces {
		select {
		case <-ctx.Done():
			return fmt.Errorf("interrupted by user (saved %d/%d pieces)", downloadedPieces, totalPieces)
		case <-seedTicker.C:
			seeded := seededBlocks.Load()
			fmt.Fprintf(os.Stderr, "\rDownloaded: %d/%d pieces | Seeded: %d blocks", downloadedPieces, totalPieces, seeded)
		case piece := <-resultQueue:
			if completedPieces[piece.Index] {
				continue
			}

			if err := pm.Write(piece.Index, piece.Data); err != nil {
				return fmt.Errorf("failed to write piece %d: %w", piece.Index, err)
			}

			// update context
			myBitfield.SetPiece(piece.Index)
			completedPieces[piece.Index] = true
			downloadedPieces++

			// tell peers we got a new piece available to seed
			swarm.UpdatePeers(piece)
			fmt.Fprintf(os.Stderr, "\rDownloaded: %d/%d pieces | Seeded: %d blocks", downloadedPieces, totalPieces, seededBlocks.Load())
		}
	}

	fmt.Fprintf(os.Stderr, "\r\033[K")
	log.Printf("Download completed in %v", time.Since(start))
	return nil
}

func main() {
	filepath := flag.String("file", "", "torrent file to download")
	port := flag.Int("port", -1, "port to listen for peers (<=0 to not listen)")
	seed := flag.Bool("seed", false, "keep running and seeding after download completes")
	directPeers := flag.String("peer", "", "IP:Port of a direct peers (separated by ',') to connect to")
	flag.IntVar(&bittorrent.MaxPipeline, "pipeline", 5, "pipeline size for peer request")

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
	log.Printf("PieceLength: %d bytes", t.PieceLength)
	log.Printf("TotalLength: %d bytes", t.TotalLength)
	log.Printf("Piece Count: %d", len(t.Pieces))
	log.Printf("Peer ID:     %x", peerID)
	log.Printf("Files (%d):", len(t.Files))
	for i, f := range t.Files {
		log.Printf("  [%d] %s (%d bytes)", i, f.Path, f.Length)
	}

	peers, err := parsePeers(*directPeers)
	if err != nil {
		log.Fatalf("failed to parse peers: %v", err)
	}

	err = download(t, peerID, trackerURL, *port, *seed, peers)
	if err != nil {
		log.Fatalf("failed download: %v", err)
	}
}
