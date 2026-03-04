package main

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"flag"
	"fmt"
	"log"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/4zv4l/gbt/bittorrent"
	"github.com/4zv4l/gbt/fs"
	"github.com/4zv4l/gbt/torrent"
)

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

// check if part of the file are already on disk
func verifyAndFillQueue(t *torrent.TorrentFile, s *bittorrent.Swarm, completedPieces map[int]bool) int {
	downloadedPieces := 0

	for i, hash := range t.Pieces {
		length := t.PieceLength
		if i == len(t.Pieces)-1 {
			if remainder := t.TotalLength % t.PieceLength; remainder != 0 {
				length = remainder
			}
		}

		data, err := s.Manager.ReadPiece(i, length)
		if err == nil && sha1.Sum(data) == hash {
			completedPieces[i] = true
			downloadedPieces++
			s.Bitfield.SetPiece(i)
		} else {
			s.WorkQueue <- bittorrent.PieceWork{
				Index:  i,
				Hash:   hash[:],
				Length: length,
			}
		}
	}

	return downloadedPieces
}

func downloadLoop(t *torrent.TorrentFile, peerID [20]byte, trackerURL string, port int, seed bool, directPeers []netip.AddrPort) error {
	var (
		totalPieces      = len(t.Pieces)
		downloadedPieces = 0
		completedPieces  = map[int]bool{}
		seededBlocks     atomic.Uint64
		workQueue        = make(chan bittorrent.PieceWork, totalPieces)
		resultQueue      = make(chan bittorrent.PieceResult, totalPieces)
		myBitfield       = bittorrent.NewSharedBitfield(totalPieces)
		ctx, cancel      = context.WithCancel(context.Background())
	)
	defer cancel()

	setupGracefulShutdown(cancel)

	// prepare files
	pm, err := fs.NewPieceManager(t.Files, t.PieceLength)
	if err != nil {
		return err
	}
	defer pm.Close()

	// setup swamp
	swarm := &bittorrent.Swarm{
		Peers:         &sync.Map{},
		WorkQueue:     workQueue,
		ResultQueue:   resultQueue,
		Manager:       pm,
		Handshake:     bittorrent.MakeHandshake(t.InfoHash, peerID),
		Bitfield:      myBitfield,
		SeededCounter: &seededBlocks,
	}

	// check if part of the file are already on disk
	downloadedPieces = verifyAndFillQueue(t, swarm, completedPieces)

	// start actively listening
	if port > 0 {
		go swarm.StartActiveSeeding(port)
	}

	// can add local peer
	if len(directPeers) > 0 {
		mockReply := bittorrent.TrackerReply{Peers: directPeers}
		swarm.AddPeer(mockReply)
	}
	// start finding peers in the background
	go swarm.TrackerLoop(trackerURL)

	// receive and write pieces
	for downloadedPieces < totalPieces {
		select {
		case <-ctx.Done():
			return fmt.Errorf("interrupted by user (saved %d/%d pieces)", downloadedPieces, totalPieces)
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

			// update peers
			swarm.UpdatePeers(piece)

			seeded := seededBlocks.Load()
			fmt.Fprintf(os.Stderr, "\rDownloaded: %d/%d pieces | Seeded: %d blocks", downloadedPieces, totalPieces, seeded)
		}
	}

	if seed {
		log.Printf("Download 100%% complete, seeding...")

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				seeded := seededBlocks.Load()
				fmt.Fprintf(os.Stderr, "\rDownloaded: %d/%d pieces | Seeded: %d blocks", downloadedPieces, totalPieces, seeded)
			}
		}
	}

	return nil
}

func parsePeers(strPeers string) ([]netip.AddrPort, error) {
	var peers []netip.AddrPort
	if strPeers == "" {
		return []netip.AddrPort{}, nil
	}

	for _, strPeer := range strings.Split(strPeers, ",") {
		peer, err := netip.ParseAddrPort(strPeer)
		if err != nil {
			return nil, err
		}
		peers = append(peers, peer)
	}
	return peers, nil
}

func main() {
	start := time.Now()
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

	err = downloadLoop(t, peerID, trackerURL, *port, *seed, peers)
	if err != nil {
		log.Fatalf("failed download: %v", err)
	}
	fmt.Println()
	log.Printf("Download completed in %v", time.Since(start))
}
