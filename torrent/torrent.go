/*
Torrent module parsing .torrent file(s)
*/
package torrent

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/4zv4l/gbt/bencode"
)

// File represents a single file inside the torrent.
type File struct {
	Length int
	Path   []string
}

type TorrentFile struct {
	Announce    string     // URL to contact tracker (if not exists will use not yet implemented DHT)
	InfoHash    [20]byte   // The raw 20-byte SHA-1 hash
	PieceLength int        // Size of each Piece
	Pieces      [][20]byte // Pieces SHA-1 hash
	Name        string     // Suggested filename or root directory name
	Files       []File     // List of all files
	TotalLength int        // Total size of all files combined
}

// Parse takes a raw byte slice of a .torrent file and extracts the data.
func Parse(data []byte) (*TorrentFile, error) {
	val, err := bencode.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	dict, ok := val.(map[string]any)
	if !ok {
		return nil, errors.New("invalid torrent: root is not a dictionary")
	}

	announce, _ := dict["announce"].(string)

	info, ok := dict["info"].(map[string]any)
	if !ok {
		return nil, errors.New("invalid torrent: missing info dictionary")
	}

	name, _ := info["name"].(string)
	pieceLength, _ := info["piece length"].(int)

	piecesStr, ok := info["pieces"].(string)
	if !ok {
		return nil, errors.New("invalid torrent: missing pieces")
	}

	piecesBytes := []byte(piecesStr)
	const hashLen = 20

	if len(piecesBytes)%hashLen != 0 {
		return nil, fmt.Errorf("invalid pieces length: %d is not a multiple of 20", len(piecesBytes))
	}

	numPieces := len(piecesBytes) / hashLen
	pieceHashes := make([][20]byte, numPieces)
	for i := range numPieces {
		offset := i * hashLen
		pieceHashes[i] = [20]byte(piecesBytes[offset : offset+hashLen])
	}

	var files []File
	var totalLength int

	if filesList, exists := info["files"]; exists {
		for _, f := range filesList.([]any) {
			fDict := f.(map[string]any)
			length, _ := fDict["length"].(int)

			var path []string
			for _, p := range fDict["path"].([]any) {
				path = append(path, p.(string))
			}

			files = append(files, File{Length: length, Path: path})
			totalLength += length
		}
	} else {
		length, ok := info["length"].(int)
		if !ok {
			return nil, errors.New("invalid torrent: no length or files list")
		}
		files = append(files, File{Length: length, Path: []string{name}})
		totalLength = length
	}

	infoHash, err := calculateInfoHash(info)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate infohash: %w", err)
	}

	return &TorrentFile{
		Announce:    announce,
		InfoHash:    infoHash,
		PieceLength: pieceLength,
		Pieces:      pieceHashes,
		Name:        name,
		Files:       files,
		TotalLength: totalLength,
	}, nil
}

// generate the url to send to the track to get peers
func (t *TorrentFile) TrackerURL(peerID [20]byte) (string, error) {
	uri, err := url.Parse(t.Announce)
	if err != nil {
		return "", err
	}
	uri.RawQuery = url.Values{
		"info_hash":  []string{string(t.InfoHash[:])},
		"peer_id":    []string{string(peerID[:])},
		"port":       []string{uri.Port()},
		"uploaded":   []string{"0"},
		"downloaded": []string{"0"},
		"compact":    []string{"1"},
		"left":       []string{strconv.Itoa(t.TotalLength)},
	}.Encode()
	return uri.String(), nil
}

// calculateInfoHash re-encodes the info dictionary and calculates its SHA-1 hash.
func calculateInfoHash(info map[string]any) ([20]byte, error) {
	var buf bytes.Buffer
	if err := bencode.Encode(&buf, info); err != nil {
		return [20]byte{}, err
	}
	return sha1.Sum(buf.Bytes()), nil
}
