package task

import (
	"errors"
	"io"
)

const (
	MaxVarIntLen  = 5   // VarInt для int32 — не длиннее 5 байт
	MaxAddressLen = 255 // лимит Handshake.Address в байтах
	MaxChatLen    = 256 // лимит Chat.Message в байтах
)

var (
	ErrVarIntTooLong      = errors.New("varint: too long")
	ErrVarIntOverflow     = errors.New("varint: overflows int32")
	ErrVarIntNonCanonical = errors.New("varint: non-canonical")
	ErrBadLength          = errors.New("frame: bad length")
	ErrFrameTooLarge      = errors.New("frame: too large")
	ErrUnknownPacket      = errors.New("packet: unknown id")
	ErrShortPacket        = errors.New("packet: too short")
	ErrStringTooLong      = errors.New("packet: string too long")
	ErrBadUTF8            = errors.New("packet: invalid utf-8")
	ErrTrailingData       = errors.New("packet: trailing data")
)

var errNotImplemented = errors.New("not implemented")

// Reader — то, из чего читаются кадры: bufio.Reader, bytes.Reader.
type Reader interface {
	io.Reader
	io.ByteReader
}

// Frame — кадр без префикса длины: id пакета и его данные.
type Frame struct {
	ID   int32
	Data []byte
}

// Пакеты учебного протокола (id 0x00, 0x01, 0x02).
type Handshake struct {
	Protocol  int32
	Address   string
	Port      uint16
	NextState int32
}

type Chat struct {
	Message string
}

type Move struct {
	X, Z float64
}

// AppendVarInt дописывает v в формате VarInt к b.
func AppendVarInt(b []byte, v int32) []byte {
	// TODO
	return b
}

// ReadVarInt читает одно число VarInt.
func ReadVarInt(r io.ByteReader) (int32, error) {
	// TODO
	return 0, errNotImplemented
}

// AppendFrame дописывает к b кадр: длина, id, данные.
func AppendFrame(b []byte, id int32, data []byte) []byte {
	// TODO
	return b
}

// ReadFrame читает один кадр, тело которого не длиннее maxFrame байт.
func ReadFrame(r Reader, maxFrame int) (Frame, error) {
	// TODO
	return Frame{}, errNotImplemented
}

// DecodePacket разбирает данные кадра в Handshake, Chat или Move.
func DecodePacket(f Frame) (any, error) {
	// TODO
	return nil, errNotImplemented
}
