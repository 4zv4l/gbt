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

	"github.com/4zv4l/gbt/torrent"
)

type FileEntry struct {
	File         *os.File
	GlobalOffset int // multiple files are seen as one big file in torrent
	Length       int
}

type PieceManager struct {
	files       []FileEntry
	pieceLength int
}

func NewPieceManager(torrentFiles []torrent.File, pieceLength int) (*PieceManager, error) {
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

	return &PieceManager{files: entries, pieceLength: pieceLength}, nil
}

func (pm *PieceManager) Close() {
	for _, entry := range pm.files {
		entry.File.Close()
	}
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
// FileOp defines the callback executed when an overlap is found.
type FileOp func(f *FileEntry, sliceStart, sliceEnd, localFileOffset int) error

// operateOnOverlaps handles the complex math of slicing a global offset across multiple file boundaries.
func (pm *PieceManager) operateOnOverlaps(globalStart, targetLength int, op FileOp) error {
	globalEnd := globalStart + targetLength

	for _, f := range pm.files {
		fileGlobalStart := f.GlobalOffset
		fileGlobalEnd := f.GlobalOffset + f.Length

		// If the requested block overlaps with this specific file
		if globalStart < fileGlobalEnd && globalEnd > fileGlobalStart {
			overlapStart := max(globalStart, fileGlobalStart)
			overlapEnd := min(globalEnd, fileGlobalEnd)

			sliceStart := overlapStart - globalStart
			sliceEnd := overlapEnd - globalStart
			localFileOffset := overlapStart - fileGlobalStart

			// Execute the specific read/write action
			if err := op(&f, sliceStart, sliceEnd, localFileOffset); err != nil {
				return err
			}
		}
	}
	return nil
}

// Write handles the complex math of slicing a piece across multiple file boundaries.
func (pm *PieceManager) Write(index int, data []byte) error {
	globalStart := index * pm.pieceLength

	return pm.operateOnOverlaps(globalStart, len(data), func(f *FileEntry, sliceStart, sliceEnd, localFileOffset int) error {
		_, err := f.File.WriteAt(data[sliceStart:sliceEnd], int64(localFileOffset))
		return err
	})
}

func (pm *PieceManager) ReadPiece(index int, length int) ([]byte, error) {
	buf := make([]byte, length)
	globalStart := index * pm.pieceLength

	err := pm.operateOnOverlaps(globalStart, length, func(f *FileEntry, sliceStart, sliceEnd, localFileOffset int) error {
		_, err := f.File.ReadAt(buf[sliceStart:sliceEnd], int64(localFileOffset))
		// if EOF, its ok, file not fully downloaded
		if err != nil && err != io.EOF {
			return err
		}
		return nil
	})

	return buf, err
}

// for seeding
func (pm *PieceManager) ReadBlock(index int, begin int, length int) ([]byte, error) {
	buf := make([]byte, length)
	globalStart := (index * pm.pieceLength) + begin
	bytesRead := 0

	err := pm.operateOnOverlaps(globalStart, length, func(f *FileEntry, sliceStart, sliceEnd, localFileOffset int) error {
		n, err := f.File.ReadAt(buf[sliceStart:sliceEnd], int64(localFileOffset))
		if err != nil && err != io.EOF {
			return err
		}
		bytesRead += n
		return nil
	})

	if err != nil {
		return nil, err
	}

	if bytesRead < length {
		return nil, fmt.Errorf("could not read the full block from disk")
	}

	return buf, nil
}
