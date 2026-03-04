package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/4zv4l/gbt/bittorrent"
	"github.com/4zv4l/gbt/fs"
	"github.com/4zv4l/gbt/torrent"
)

func addPeer(reply bittorrent.TrackerReply, peers *sync.Map, handshake bittorrent.Handshake, workQueue chan bittorrent.PieceWork, resultQueue chan bittorrent.PieceResult) {
	for _, peer := range reply.Peers {
		if _, exists := peers.Load(peer); exists {
			continue
		}
		peers.Store(peer, true)

		go func(p netip.AddrPort) {
			conn, err := bittorrent.ConnectAndHandshake(p, handshake)
			if err != nil {
				peers.Delete(p)
				return
			}
			slog.Info("successful handshake", "conn", conn.RemoteAddr())
			go bittorrent.PeerWorker(conn, workQueue, resultQueue)
		}(peer)
	}
}

// trackerLoop will request new peers from tracker and start a goroutine for the new
// peers to listen for new pieces to download
func trackerLoop(trackerURL string, handshake bittorrent.Handshake, peers *sync.Map, workQueue chan bittorrent.PieceWork, resultQueue chan bittorrent.PieceResult) error {
	reply, err := bittorrent.GetPeersFromTracker(trackerURL)
	if err != nil {
		return err
	}
	slog.Info("got peers from tracker", "peers", reply.Peers)
	addPeer(reply, peers, handshake, workQueue, resultQueue)
	slog.Info("filled peers map")

	ticker := time.NewTicker(time.Duration(reply.Interval) * time.Second)

	for {
		<-ticker.C
		reply, err := bittorrent.GetPeersFromTracker(trackerURL)
		if err != nil {
			return err
		}
		slog.Info("got peers from tracker", "peers", reply.Peers)
		addPeer(reply, peers, handshake, workQueue, resultQueue)
		slog.Info("filled peers map")
	}
}

func downloadLoop(t *torrent.TorrentFile, peerID [20]byte, trackerURL string) error {
	var (
		totalPieces      = len(t.Pieces)
		downloadedPieces = 0
		completedPieces  = map[int]bool{}
		workQueue        = make(chan bittorrent.PieceWork, totalPieces)
		resultQueue      = make(chan bittorrent.PieceResult, totalPieces)
		peers            = sync.Map{}
	)

	// fill workQueue will all pieces that needs to be downloaded
	for i, hash := range t.Pieces {
		length := t.PieceLength

		// If it's the very last piece, calculate the remainder
		if i == len(t.Pieces)-1 {
			remainder := t.TotalLength % t.PieceLength
			if remainder != 0 {
				length = remainder
			}
		}
		workQueue <- bittorrent.PieceWork{
			Index:  i,
			Hash:   hash[:],
			Length: length,
		}
	}
	slog.Info("filled workQueue")

	// start finding peers in the background
	go trackerLoop(trackerURL, bittorrent.MakeHandshake(t.InfoHash, peerID), &peers, workQueue, resultQueue)

	// prepare files
	writer, err := fs.NewPiecesWriter(t.Files, t.PieceLength)
	if err != nil {
		return err
	}
	defer writer.Close()

	// receive and write pieces
	for downloadedPieces < totalPieces {
		piece := <-resultQueue
		slog.Info("got a piece from a worker", "piece", piece)
		if completedPieces[piece.Index] {
			continue
		}

		if err := writer.Write(piece); err != nil {
			return fmt.Errorf("failed to write piece %d: %w", piece.Index, err)
		}
		// TODO change Bitfield to have the piece at 1

		completedPieces[piece.Index] = true
		downloadedPieces++
		slog.Info("Downloaded a piece!", "downloadedPieces", downloadedPieces, "totalPieces", totalPieces)
	}

	return nil
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
	slog.Info("download completed !")
}
