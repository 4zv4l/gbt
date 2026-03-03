package bittorrent

import (
	"bytes"
	"encoding/binary"
	"io"
)

type BtMsg uint8

const (
	MsgChoke         BtMsg = 0
	MsgUnchoke       BtMsg = 1
	MsgInterested    BtMsg = 2
	MsgNotInterested BtMsg = 3
	MsgHave          BtMsg = 4
	MsgBitfield      BtMsg = 5
	MsgRequest       BtMsg = 6
	MsgPiece         BtMsg = 7
	MsgCancel        BtMsg = 8
)

func (btmsg BtMsg) String() string {
	return []string{
		"MsgChoke",
		"MsgUnchoke",
		"MsgInterested",
		"MsgNotInterested",
		"MsgHave",
		"MsgBitfield",
		"MsgRequest",
		"MsgPiece",
		"MsgCancel",
	}[btmsg]
}

type Message struct {
	ID      BtMsg
	Payload []byte
}

func (m Message) ToByte() []byte {
	buf := make([]byte, 4+1+len(m.Payload))
	binary.BigEndian.PutUint32(buf[:4], uint32(1+len(m.Payload)))
	buf[4] = byte(m.ID)
	copy(buf[5:], m.Payload)
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

	payloadBuf := make([]byte, length)
	_, err = io.ReadFull(r, payloadBuf)
	if err != nil {
		return nil, err
	}

	return &Message{ID: BtMsg(payloadBuf[0]), Payload: payloadBuf[1:]}, nil
}

func (m Message) WriteMessage(w io.Writer) error {
	_, err := io.Copy(w, bytes.NewReader(m.ToByte()))
	return err
}
