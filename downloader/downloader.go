package downloader

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/4zv4l/gbt/bittorrent"
	"github.com/4zv4l/gbt/fs"
	"github.com/4zv4l/gbt/torrent"
)

// Progress is the data sent back via channel
// with Download
type Progress struct {
	DownloadedPieces int
	TotalPieces      int
	SeededBlocks     uint64
}

func downloadLoop(
	ctx context.Context,
	downloadedPieces, totalPieces int,
	seededBlocks *atomic.Uint64,
	resultQueue chan bittorrent.PieceResult,
	progressCH chan Progress,
	errCH chan error,
	completedPieces map[int]bool,
	swarm *bittorrent.Swarm,
	seed bool,
) {
	seedTicker := time.NewTicker(500 * time.Millisecond) // tick every 500ms to update download/upload status

	for seed || downloadedPieces < totalPieces {
		select {
		case <-ctx.Done():
			errCH <- fmt.Errorf("download interrupted (saved %d/%d pieces)", downloadedPieces, totalPieces)
			return
		case <-seedTicker.C:
			progressCH <- Progress{
				DownloadedPieces: downloadedPieces,
				TotalPieces:      totalPieces,
				SeededBlocks:     seededBlocks.Load(),
			}
		case piece := <-resultQueue:
			if completedPieces[piece.Index] {
				continue
			}

			if err := swarm.Manager.Write(piece.Index, piece.Data); err != nil {
				errCH <- fmt.Errorf("failed to write piece %d: %w", piece.Index, err)
				return
			}

			// update context
			swarm.Bitfield.SetPiece(piece.Index)
			completedPieces[piece.Index] = true
			downloadedPieces++

			// tell peers we got a new piece available to seed
			swarm.UpdatePeers(piece)

			progressCH <- Progress{
				DownloadedPieces: downloadedPieces,
				TotalPieces:      totalPieces,
				SeededBlocks:     seededBlocks.Load(),
			}
		}
	}
}

func Download(
	ctx context.Context,
	t *torrent.TorrentFile,
	peerID [20]byte,
	trackerURL string,
	pipelineSize, port int,
	seed bool,
	directPeers []netip.AddrPort,
) (chan Progress, chan error) {
	var (
		totalPieces      = len(t.Pieces)
		downloadedPieces = 0
		completedPieces  = map[int]bool{}
		seededBlocks     atomic.Uint64
		workQueue        = make(chan bittorrent.PieceWork, totalPieces)
		resultQueue      = make(chan bittorrent.PieceResult, totalPieces)
		progressCH       = make(chan Progress)
		errCH            = make(chan error, 1)
		myBitfield       = bittorrent.NewSharedBitfield(totalPieces)
	)

	go func() {
		defer close(progressCH)

		// prepare files (pre-create)
		pm, err := fs.NewPieceManager(t.Files, t.PieceLength)
		if err != nil {
			return
		}
		defer pm.Close()

		// setup swamp that handles the workers
		// and resume download if part of the file are already on disk
		swarm := (&bittorrent.Swarm{
			Peers:         &sync.Map{},
			WorkQueue:     workQueue,
			ResultQueue:   resultQueue,
			Manager:       pm,
			Handshake:     bittorrent.MakeHandshake(t.InfoHash, peerID),
			Bitfield:      myBitfield,
			SeededCounter: &seededBlocks,
			MaxPipeline:   pipelineSize,
		}).WithResume(t, completedPieces, &downloadedPieces)

		// start actively listening
		if port > 0 {
			go swarm.StartActiveSeeding(port)
		}

		// getting peers, local through cli or through tracker
		swarm.AddPeer(directPeers)
		go swarm.TrackerLoop(trackerURL)

		// receive and write pieces
		downloadLoop(
			ctx,
			downloadedPieces, totalPieces,
			&seededBlocks,
			resultQueue,
			progressCH,
			errCH,
			completedPieces,
			swarm,
			seed,
		)
	}()

	return progressCH, errCH
}
