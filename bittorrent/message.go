package bittorrent

import (
	"encoding/binary"
	"io"
)

const (
	MsgChoke         uint8 = 0
	MsgUnchoke       uint8 = 1
	MsgInterested    uint8 = 2
	MsgNotInterested uint8 = 3
	MsgHave          uint8 = 4
	MsgBitfield      uint8 = 5
	MsgRequest       uint8 = 6
	MsgPiece         uint8 = 7
	MsgCancel        uint8 = 8
)

type Message struct {
	ID      uint8
	Payload []byte
}

func (m Message) ToByte() []byte {
	buf := make([]byte, 4+1+len(m.Payload))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(m.Payload)))
	buf[4] = byte(m.ID)
	copy(buf[4:], m.Payload)
	return buf
}

func ReadMessage(r io.Reader) (*Message, error) {
	var lenBuf [4]byte
	_, err := io.ReadFull(r, lenBuf[:])
	if err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 { // keep-alive msg
		return nil, nil
	}

	payloadBuf := make([]byte, length+1)
	_, err = io.ReadFull(r, payloadBuf)
	if err != nil {
		return nil, err
	}

	return &Message{ID: payloadBuf[0], Payload: payloadBuf[1:]}, nil
}

// https://blog.jse.li/posts/torrent/
// A Bitfield represents the pieces that a peer has
type Bitfield []byte

// HasPiece tells if a bitfield has a particular index set
func (bf Bitfield) HasPiece(index int) bool {
	byteIndex := index / 8
	offset := index % 8
	return bf[byteIndex]>>(7-offset)&1 != 0
}

// SetPiece sets a bit in the bitfield
func (bf Bitfield) SetPiece(index int) {
	byteIndex := index / 8
	offset := index % 8
	bf[byteIndex] |= 1 << (7 - offset)
}
