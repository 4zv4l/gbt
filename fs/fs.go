/*
module responsible to handle write/read piece to disk
- writing pieces to disk (handle overlap files)
- reload pieces from disk (restarting a download)
*/
package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/4zv4l/gbt/bittorrent"
	"github.com/4zv4l/gbt/torrent"
)

type FileEntry struct {
	File         *os.File
	GlobalOffset int // multiple files are seen as one big file in torrent
	Length       int
}

type PieceWriter struct {
	files       []FileEntry
	pieceLength int
}

func NewPiecesWriter(torrentFiles []torrent.File, pieceLength int) (*PieceWriter, error) {
	var (
		entries       = []FileEntry{}
		currentOffset = 0
	)

	for _, f := range torrentFiles {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0755); err != nil {
			return nil, fmt.Errorf("couldnt create directory for %s: %w", f.Path, err)
		}
		file, err := os.OpenFile(f.Path, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return nil, fmt.Errorf("couldnt create file %s: %w", f.Path, err)
		}
		entries = append(entries, FileEntry{File: file, GlobalOffset: currentOffset, Length: f.Length})
		currentOffset += f.Length
	}

	return &PieceWriter{files: entries, pieceLength: pieceLength}, nil
}

/*
GLOBAL SPACE (Bytes)
0                  80       100      130                 200
|===================|========|========|===================|
[              FILE A        ][              FILE B       ]
                    [       PIECE     ]

                    OVERLAP 1 (File A)
                    [========]
                             OVERLAP 2 (File B)
                             [========]

overlapStart    := max(pieceGlobalStart (80), fileGlobalStart (100)) // Result: 100
overlapEnd      := min(pieceGlobalEnd (130), fileGlobalEnd (200))    // Result: 130
sliceStart      := overlapStart (100) - pieceGlobalStart (80) 		// Result: 20
sliceEnd        := overlapEnd (130)   - pieceGlobalStart (80) 		// Result: 50
dataToWrite     := piece.Data[20:50]
localFileOffset := overlapStart (100) - fileGlobalStart (100) 		// Result: 0
*/
// Write handles the complex math of slicing a piece across multiple file boundaries.
func (w *PieceWriter) Write(piece bittorrent.PieceResult) error {
	var (
		pieceGlobalStart = piece.Index * w.pieceLength
		pieceGlobalEnd   = pieceGlobalStart + len(piece.Data)
	)

	for _, f := range w.files {
		fileGlobalStart := f.GlobalOffset
		fileGlobalEnd := f.GlobalOffset + f.Length

		// If the piece overlaps with this specific file
		if pieceGlobalStart < fileGlobalEnd && pieceGlobalEnd > fileGlobalStart {
			overlapStart := max(pieceGlobalStart, fileGlobalStart)
			overlapEnd := min(pieceGlobalEnd, fileGlobalEnd)

			sliceStart := overlapStart - pieceGlobalStart
			sliceEnd := overlapEnd - pieceGlobalStart
			dataToWrite := piece.Data[sliceStart:sliceEnd]

			localFileOffset := overlapStart - fileGlobalStart

			if _, err := f.File.WriteAt(dataToWrite, int64(localFileOffset)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *PieceWriter) ReadPiece(index int, length int) ([]byte, error) {
	var (
		buf              = make([]byte, length)
		pieceGlobalStart = index * w.pieceLength
		pieceGlobalEnd   = pieceGlobalStart + length
	)

	for _, f := range w.files {
		fileGlobalStart := f.GlobalOffset
		fileGlobalEnd := f.GlobalOffset + f.Length

		if pieceGlobalStart < fileGlobalEnd && pieceGlobalEnd > fileGlobalStart {
			overlapStart := max(pieceGlobalStart, fileGlobalStart)
			overlapEnd := min(pieceGlobalEnd, fileGlobalEnd)

			sliceStart := overlapStart - pieceGlobalStart
			sliceEnd := overlapEnd - pieceGlobalStart
			localFileOffset := overlapStart - fileGlobalStart

			_, err := f.File.ReadAt(buf[sliceStart:sliceEnd], int64(localFileOffset))
			// if EOF, its ok, file not fully downloaded
			if err != nil && err != io.EOF {
				return nil, err
			}
		}
	}
	return buf, nil
}

func (w *PieceWriter) Close() {
	for _, entry := range w.files {
		entry.File.Close()
	}
}
