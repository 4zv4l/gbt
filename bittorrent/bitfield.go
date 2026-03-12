package bittorrent

import "sync"

type Bitfield []byte

func (bf Bitfield) HasPiece(index int) bool {
	byteIndex := index / 8
	offset := index % 8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return false
	}
	return bf[byteIndex]>>(7-offset)&1 != 0
}

func (bf Bitfield) SetPiece(index int) {
	byteIndex := index / 8
	offset := index % 8
	bf[byteIndex] |= 1 << (7 - offset)
}

// Thread-safe Bitfield
type SharedBitfield struct {
	mu   sync.RWMutex
	data []byte
}

func NewSharedBitfield(totalPieces int) *SharedBitfield {
	// (totalPieces + 7) / 8 is a quick way to do math.Ceil(totalPieces/8)
	return &SharedBitfield{
		data: make([]byte, (totalPieces+7)/8),
	}
}

func (sb *SharedBitfield) SetPiece(index int) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	byteIndex := index / 8
	offset := index % 8
	if byteIndex >= 0 && byteIndex < len(sb.data) {
		sb.data[byteIndex] |= 1 << (7 - offset)
	}
}

func (sb *SharedBitfield) ToByte() []byte {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	buf := make([]byte, len(sb.data))
	copy(buf, sb.data)
	return buf
}
