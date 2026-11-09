package task

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"unicode/utf8"
)

const (
	MaxVarIntLen  = 5
	MaxAddressLen = 255
	MaxChatLen    = 256
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

type Reader interface {
	io.Reader
	io.ByteReader
}

type Frame struct {
	ID   int32
	Data []byte
}

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

func AppendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

func ReadVarInt(r io.ByteReader) (int32, error) {
	var u uint32
	for i := range MaxVarIntLen {
		c, err := r.ReadByte()
		if err != nil {
			if i > 0 && err == io.EOF {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, err
		}
		if i == MaxVarIntLen-1 {
			if c&0x80 != 0 {
				return 0, ErrVarIntTooLong
			}
			if c > 0x0f {
				return 0, ErrVarIntOverflow
			}
		}
		u |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			if i > 0 && c == 0 {
				return 0, ErrVarIntNonCanonical
			}
			return int32(u), nil
		}
	}
	return 0, ErrVarIntTooLong // недостижимо: пятый байт всегда завершает число
}

func AppendFrame(b []byte, id int32, data []byte) []byte {
	var idBuf [MaxVarIntLen]byte
	idBytes := AppendVarInt(idBuf[:0], id)
	b = AppendVarInt(b, int32(len(idBytes)+len(data)))
	b = append(b, idBytes...)
	return append(b, data...)
}

func ReadFrame(r Reader, maxFrame int) (Frame, error) {
	n, err := ReadVarInt(r)
	if err != nil {
		return Frame{}, err
	}
	if n < 1 {
		return Frame{}, ErrBadLength
	}
	if int(n) > maxFrame {
		return Frame{}, ErrFrameTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	br := bytes.NewReader(body)
	id, err := ReadVarInt(br)
	if err != nil {
		if err == io.ErrUnexpectedEOF {
			err = ErrBadLength
		}
		return Frame{}, err
	}
	return Frame{ID: id, Data: body[len(body)-br.Len():]}, nil
}

// fields — курсор по данным пакета: каждое поле проверяет, что байтов
// хватает, и сдвигает срез.
type fields struct{ b []byte }

func (f *fields) varint() (int32, error) {
	br := bytes.NewReader(f.b)
	v, err := ReadVarInt(br)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return 0, ErrShortPacket
	}
	if err != nil {
		return 0, err
	}
	f.b = f.b[len(f.b)-br.Len():]
	return v, nil
}

func (f *fields) take(n int) ([]byte, error) {
	if n > len(f.b) {
		return nil, ErrShortPacket
	}
	v := f.b[:n]
	f.b = f.b[n:]
	return v, nil
}

func (f *fields) str(max int) (string, error) {
	n, err := f.varint()
	if err != nil {
		return "", err
	}
	if n < 0 {
		return "", ErrBadLength
	}
	if int(n) > max {
		return "", ErrStringTooLong
	}
	b, err := f.take(int(n))
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", ErrBadUTF8
	}
	return string(b), nil
}

func (f *fields) float64() (float64, error) {
	b, err := f.take(8)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
}

func DecodePacket(f Frame) (any, error) {
	in := &fields{b: f.Data}
	var p any
	var err error
	switch f.ID {
	case 0x00:
		var h Handshake
		if h.Protocol, err = in.varint(); err != nil {
			return nil, err
		}
		if h.Address, err = in.str(MaxAddressLen); err != nil {
			return nil, err
		}
		port, err := in.take(2)
		if err != nil {
			return nil, err
		}
		h.Port = binary.BigEndian.Uint16(port)
		if h.NextState, err = in.varint(); err != nil {
			return nil, err
		}
		p = h
	case 0x01:
		var c Chat
		if c.Message, err = in.str(MaxChatLen); err != nil {
			return nil, err
		}
		p = c
	case 0x02:
		var m Move
		if m.X, err = in.float64(); err != nil {
			return nil, err
		}
		if m.Z, err = in.float64(); err != nil {
			return nil, err
		}
		p = m
	default:
		return nil, ErrUnknownPacket
	}
	if len(in.b) != 0 {
		return nil, ErrTrailingData
	}
	return p, nil
}
