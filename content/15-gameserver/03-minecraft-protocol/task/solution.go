package task

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
)

const (
	MaxPacketLength = 2097151 // 2²¹−1: длина кадра — не больше 3 байт VarInt
	MaxVarIntLen    = 5
)

var (
	ErrVarInt         = errors.New("bad varint")
	ErrBadLength      = errors.New("bad length")
	ErrTooLarge       = errors.New("packet too large")
	ErrBelowThreshold = errors.New("compressed packet below threshold")
	ErrBadCompression = errors.New("bad zlib data")
	ErrSizeMismatch   = errors.New("decompressed size mismatch")
)

type Reader interface {
	io.Reader
	io.ByteReader
}

func appendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for ; u >= 0x80; u >>= 7 {
		b = append(b, byte(u)|0x80)
	}
	return append(b, byte(u))
}

func readVarInt(r io.ByteReader) (int32, error) {
	var u uint32
	for i := range MaxVarIntLen {
		c, err := r.ReadByte()
		if err != nil {
			if i > 0 && err == io.EOF {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, err
		}
		u |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return int32(u), nil
		}
	}
	return 0, ErrVarInt
}

func AppendCompressed(dst, packet []byte, threshold int) ([]byte, error) {
	var head [MaxVarIntLen]byte
	var body []byte
	if len(packet) < threshold {
		body = append(appendVarInt(head[:0], 0), packet...)
	} else {
		var z bytes.Buffer
		z.Write(appendVarInt(head[:0], int32(len(packet))))
		zw := zlib.NewWriter(&z)
		zw.Write(packet)
		if err := zw.Close(); err != nil {
			return dst, err
		}
		body = z.Bytes()
	}
	if len(body) > MaxPacketLength {
		return dst, ErrTooLarge
	}
	dst = appendVarInt(dst, int32(len(body)))
	return append(dst, body...), nil
}

func ReadCompressed(r Reader, threshold, maxData int) ([]byte, error) {
	n, err := readVarInt(r)
	if err != nil {
		return nil, err
	}
	if n < 1 {
		return nil, ErrBadLength
	}
	if n > MaxPacketLength {
		return nil, ErrTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	br := bytes.NewReader(body)
	dataLen, err := readVarInt(br)
	if err != nil {
		if err == io.ErrUnexpectedEOF {
			return nil, ErrBadLength
		}
		return nil, err
	}
	if dataLen == 0 {
		if br.Len() == 0 {
			return nil, ErrBadLength
		}
		return body[len(body)-br.Len():], nil
	}
	if dataLen < 0 {
		return nil, ErrBadLength
	}
	if int(dataLen) < threshold {
		return nil, ErrBelowThreshold
	}
	if int(dataLen) > maxData {
		return nil, ErrTooLarge
	}
	zr, err := zlib.NewReader(br)
	if err != nil {
		return nil, ErrBadCompression
	}
	defer zr.Close()
	out := make([]byte, 0, dataLen)
	buf := bytes.NewBuffer(out)
	if _, err := io.Copy(buf, io.LimitReader(zr, int64(dataLen)+1)); err != nil {
		return nil, ErrBadCompression
	}
	if buf.Len() != int(dataLen) {
		return nil, ErrSizeMismatch
	}
	return buf.Bytes(), nil
}
