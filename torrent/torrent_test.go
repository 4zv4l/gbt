package torrent

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseDebianSingleFile(t *testing.T) {
	data, err := os.ReadFile("testdata/debian-13.3.0-amd64-netinst.iso.torrent")
	if err != nil {
		t.Fatalf("Failed to read debian torrent: %v", err)
	}

	tf, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	t.Logf("=== Parsed Torrent: %s ===", tf.Name)
	t.Logf("Announce:    %s", tf.Announce)
	t.Logf("InfoHash:    %x", tf.InfoHash) // %x automatically converts bytes to Hex!
	t.Logf("PieceLength: %d bytes", tf.PieceLength)
	t.Logf("TotalLength: %d bytes", tf.TotalLength)
	t.Logf("Piece Count: %d", len(tf.Pieces))
	t.Logf("Files (%d):", len(tf.Files))
	for i, f := range tf.Files {
		t.Logf("  [%d] %s (%d bytes)", i, strings.Join(f.Path, "/"), f.Length)
	}
	t.Logf("=================================")

	if expected := "http://bttracker.debian.org:6969/announce"; tf.Announce != expected {
		t.Errorf("Expected Announce %q, got %q", expected, tf.Announce)
	}
	if expected := "debian-13.3.0-amd64-netinst.iso"; tf.Name != expected {
		t.Errorf("Expected Name %q, got %q", expected, tf.Name)
	}

	if expected := 262144; tf.PieceLength != expected {
		t.Errorf("Expected PieceLength %d, got %d", expected, tf.PieceLength)
	}
	if expectedPieces := 3016; len(tf.Pieces) != expectedPieces {
		t.Errorf("Expected %d pieces, got %d", expectedPieces, len(tf.Pieces))
	}

	if len(tf.Files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(tf.Files))
	}
	if expectedSize := 790626304; tf.TotalLength != expectedSize {
		t.Errorf("Expected TotalLength %d, got %d", expectedSize, tf.TotalLength)
	}
	if tf.Files[0].Length != tf.TotalLength {
		t.Errorf("Single file length (%d) does not match TotalLength (%d)", tf.Files[0].Length, tf.TotalLength)
	}

	var emptyHash [20]byte
	if tf.InfoHash == emptyHash {
		t.Error("InfoHash is completely empty (all zeros)")
	}
}

func TestParseTmpMultiFile(t *testing.T) {
	data, err := os.ReadFile("testdata/multi-file.torrent")
	if err != nil {
		t.Fatalf("Failed to read tmp.torrent: %v", err)
	}

	tf, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}

	t.Logf("=== Parsed Torrent: %s ===", tf.Name)
	t.Logf("Announce:    %s", tf.Announce)
	t.Logf("InfoHash:    %x", tf.InfoHash)
	t.Logf("PieceLength: %d bytes", tf.PieceLength)
	t.Logf("TotalLength: %d bytes", tf.TotalLength)
	t.Logf("Piece Count: %d", len(tf.Pieces))
	t.Logf("Files (%d):", len(tf.Files))
	for i, f := range tf.Files {
		t.Logf("  [%d] %s (%d bytes)", i, strings.Join(f.Path, "/"), f.Length)
	}
	t.Logf("=================================")

	if tf.Announce != "" {
		t.Errorf("Expected empty Announce, got %q", tf.Announce)
	}
	if expected := "tmp"; tf.Name != expected {
		t.Errorf("Expected Name %q, got %q", expected, tf.Name)
	}

	if expected := 32768; tf.PieceLength != expected {
		t.Errorf("Expected PieceLength %d, got %d", expected, tf.PieceLength)
	}
	if expectedPieces := 1; len(tf.Pieces) != expectedPieces {
		t.Errorf("Expected %d piece, got %d", expectedPieces, len(tf.Pieces))
	}

	if len(tf.Files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(tf.Files))
	}

	// File 0: foobar (0 bytes)
	if tf.Files[0].Length != 0 {
		t.Errorf("Expected file 0 length to be 0, got %d", tf.Files[0].Length)
	}
	if expectedPath := []string{"foobar"}; !reflect.DeepEqual(tf.Files[0].Path, expectedPath) {
		t.Errorf("Expected file 0 path %v, got %v", expectedPath, tf.Files[0].Path)
	}

	// File 1: test-libre-office.md (192 bytes)
	if tf.Files[1].Length != 192 {
		t.Errorf("Expected file 1 length to be 192, got %d", tf.Files[1].Length)
	}
	if expectedPath := []string{"test-libre-office.md"}; !reflect.DeepEqual(tf.Files[1].Path, expectedPath) {
		t.Errorf("Expected file 1 path %v, got %v", expectedPath, tf.Files[1].Path)
	}

	if expectedTotal := 192; tf.TotalLength != expectedTotal {
		t.Errorf("Expected TotalLength %d, got %d", expectedTotal, tf.TotalLength)
	}
}
