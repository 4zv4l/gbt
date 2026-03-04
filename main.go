package main

import (
	"crypto/rand"
	"crypto/sha1"
	"flag"
	"fmt"
	"log"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/4zv4l/gbt/bittorrent"
	"github.com/4zv4l/gbt/fs"
	"github.com/4zv4l/gbt/torrent"
)

// addPeer adds peer to the thread safe peers map and start the peer handling
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
			go bittorrent.PeerWorker(conn, workQueue, resultQueue)
		}(peer)
	}
}

// trackerLoop will request new peers from tracker and start a goroutine for each new peer to be ready to download
func trackerLoop(trackerURL string, handshake bittorrent.Handshake, peers *sync.Map, workQueue chan bittorrent.PieceWork, resultQueue chan bittorrent.PieceResult) error {
	reply, err := bittorrent.GetPeersFromTracker(trackerURL)
	if err != nil {
		return err
	}
	addPeer(reply, peers, handshake, workQueue, resultQueue)

	ticker := time.NewTicker(time.Duration(reply.Interval) * time.Second)

	for {
		<-ticker.C
		reply, err := bittorrent.GetPeersFromTracker(trackerURL)
		if err != nil {
			return err
		}
		addPeer(reply, peers, handshake, workQueue, resultQueue)
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

	// prepare files
	writer, err := fs.NewPiecesWriter(t.Files, t.PieceLength)
	if err != nil {
		return err
	}
	defer writer.Close()

	// fill workQueue will all pieces that needs to be downloaded
	for i, hash := range t.Pieces {
		length := t.PieceLength

		// if it's the very last piece, calculate the remainder
		if i == len(t.Pieces)-1 {
			remainder := t.TotalLength % t.PieceLength
			if remainder != 0 {
				length = remainder
			}
		}

		data, err := writer.ReadPiece(i, length)
		if err == nil && sha1.Sum(data) == hash {
			completedPieces[i] = true
			downloadedPieces++
		} else {
			workQueue <- bittorrent.PieceWork{
				Index:  i,
				Hash:   hash[:],
				Length: length,
			}
		}
	}

	// start finding peers in the background
	go trackerLoop(trackerURL, bittorrent.MakeHandshake(t.InfoHash, peerID), &peers, workQueue, resultQueue)

	// receive and write pieces
	for downloadedPieces < totalPieces {
		piece := <-resultQueue
		if completedPieces[piece.Index] {
			continue
		}

		if err := writer.Write(piece); err != nil {
			return fmt.Errorf("failed to write piece %d: %w", piece.Index, err)
		}
		// TODO change Bitfield to have the piece at 1
		// will allow to seed afterward

		completedPieces[piece.Index] = true
		downloadedPieces++
		fmt.Fprintf(os.Stderr, "\rDownloaded %d/%d pieces", downloadedPieces, totalPieces)
	}

	return nil
}

func main() {
	var (
		filepath = flag.String("file", "", "torrent file to download")
	)

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
	log.Printf("infoHash: %x", t.InfoHash)

	var peerID [20]byte
	rand.Read(peerID[:])
	log.Printf("peer ID: %x", peerID)

	trackerURL, err := t.TrackerURL(peerID)
	if err != nil {
		log.Fatalf("couldnt generate tracker URL: %v", err)
	}

	err = downloadLoop(t, peerID, trackerURL)
	if err != nil {
		log.Fatalf("failed download: %v", err)
	}
	log.Println("download completed !")
}
