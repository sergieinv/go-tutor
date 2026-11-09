package mcproto

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
)

// MaxFrameLen — предел длины кадра в протоколе: длина кодируется VarInt
// не длиннее 3 байт, то есть 2^21-1. Сервер ставит свои пределы меньше.
const MaxFrameLen = 1<<21 - 1

var (
	ErrFrameTooBig   = errors.New("mcproto: frame is too big")
	ErrBadCompressed = errors.New("mcproto: bad compressed frame")
	ErrEmptyFrame    = errors.New("mcproto: empty frame")
)

// skippedPacket — тело «пропущенного» пакета: VarInt -1 как id.
var skippedPacket = AppendVarInt(nil, -1)

// FrameReader читает кадры из потока: VarInt-длина, затем тело. После
// включения сжатия (SetCompression) тело — VarInt «длина распакованных
// данных» (0 — не сжато) и сами данные, сжатые zlib.
//
// Защита от клиента: длина кадра не больше MaxLen, распакованный размер
// не больше MaxData (иначе маленький кадр распаковался бы в гигабайты —
// «zip-бомба»), заявленный размер обязан совпасть с фактическим.
//
// Возвращаемый срез действителен до следующего вызова Next: буферы
// переиспользуются.
type FrameReader struct {
	r         *bufio.Reader
	MaxLen    int // предел длины кадра в байтах (сжатого)
	MaxData   int // предел распакованного пакета
	threshold int // -1 — сжатие выключено
	// SkipCompressed — не распаковывать сжатые пакеты, а возвращать
	// id = -1 (нагрузочным ботам чанки не нужны: экономим их CPU).
	SkipCompressed bool

	buf  []byte
	data []byte
	zr   io.ReadCloser
	src  bytes.Reader
}

// NewFrameReader — чтение без сжатия с пределом maxLen на кадр.
func NewFrameReader(r io.Reader, maxLen int) *FrameReader {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReaderSize(r, 4096)
	}
	return &FrameReader{r: br, MaxLen: maxLen, MaxData: maxLen, threshold: -1}
}

// SetCompression включает сжатие с порогом threshold (после пакета Set
// Compression). threshold < 0 — выключить.
func (f *FrameReader) SetCompression(threshold int) { f.threshold = threshold }

// readVarInt читает VarInt длины прямо из потока.
func readVarInt(r io.ByteReader, maxBytes int) (int32, error) {
	var u uint32
	for i := 0; i < maxBytes; i++ {
		c, err := r.ReadByte()
		if err != nil {
			if i > 0 && err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		u |= uint32(c&0x7F) << (7 * i)
		if c&0x80 == 0 {
			return int32(u), nil
		}
	}
	return 0, ErrVarIntTooBig
}

// Next читает следующий пакет и возвращает его id и полезную нагрузку.
func (f *FrameReader) Next() (id int32, payload []byte, err error) {
	n, err := readVarInt(f.r, 3)
	if err != nil {
		return 0, nil, err
	}
	if n <= 0 {
		return 0, nil, ErrEmptyFrame
	}
	if int(n) > f.MaxLen {
		return 0, nil, fmt.Errorf("%w: %d > %d", ErrFrameTooBig, n, f.MaxLen)
	}
	if cap(f.buf) < int(n) {
		f.buf = make([]byte, n)
	}
	f.buf = f.buf[:n]
	if _, err := io.ReadFull(f.r, f.buf); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, err
	}
	body := f.buf
	if f.threshold >= 0 {
		body, err = f.inflate(body)
		if err != nil {
			return 0, nil, err
		}
	}
	d := NewDecoder(body)
	id = d.VarInt()
	if err := d.Err(); err != nil {
		return 0, nil, err
	}
	return id, d.Rest(), nil
}

// inflate разбирает тело сжатого режима и распаковывает его при
// необходимости.
func (f *FrameReader) inflate(frame []byte) ([]byte, error) {
	d := NewDecoder(frame)
	dataLen := d.VarInt()
	if err := d.Err(); err != nil {
		return nil, err
	}
	rest := d.Rest()
	if dataLen == 0 { // не сжато
		return rest, nil
	}
	if f.SkipCompressed {
		return skippedPacket, nil
	}
	// Сжатый пакет короче порога — нарушение протокола (так поступает и
	// ванильный сервер); заодно отсекаем отрицательные длины.
	if dataLen < int32(f.threshold) || dataLen < 0 {
		return nil, ErrBadCompressed
	}
	if int(dataLen) > f.MaxData {
		return nil, fmt.Errorf("%w: uncompressed %d > %d", ErrFrameTooBig, dataLen, f.MaxData)
	}
	f.src.Reset(rest)
	if f.zr == nil {
		zr, err := zlib.NewReader(&f.src)
		if err != nil {
			return nil, ErrBadCompressed
		}
		f.zr = zr
	} else if err := f.zr.(zlib.Resetter).Reset(&f.src, nil); err != nil {
		return nil, ErrBadCompressed
	}
	if cap(f.data) < int(dataLen) {
		f.data = make([]byte, dataLen)
	}
	f.data = f.data[:dataLen]
	if _, err := io.ReadFull(f.zr, f.data); err != nil {
		return nil, ErrBadCompressed
	}
	// Данных больше заявленного — тоже ошибка: читаем ещё байт и ждём EOF.
	var one [1]byte
	if n, _ := f.zr.Read(one[:]); n != 0 {
		return nil, ErrBadCompressed
	}
	return f.data, nil
}

// PacketWriter собирает пакеты и кадрирует их. Один PacketWriter
// принадлежит одной горутине: внутри переиспользуемый буфер тела и
// zlib-компрессор.
//
//	b := pw.Begin(id)
//	b = mcproto.AppendDouble(b, x) ...
//	dst = pw.Frame(dst, b)
type PacketWriter struct {
	threshold int // -1 — без сжатия
	body      []byte
	zbuf      bytes.Buffer
	zw        *zlib.Writer
}

// NewPacketWriter — threshold < 0 выключает сжатие.
func NewPacketWriter(threshold int) *PacketWriter {
	return &PacketWriter{threshold: threshold, body: make([]byte, 0, 1024)}
}

// Threshold — порог сжатия (-1 — выключено).
func (w *PacketWriter) Threshold() int { return w.threshold }

// SetThreshold меняет порог (после отправки Set Compression).
func (w *PacketWriter) SetThreshold(t int) { w.threshold = t }

// Begin начинает пакет с данным id и возвращает буфер тела.
func (w *PacketWriter) Begin(id int32) []byte {
	return AppendVarInt(w.body[:0], id)
}

// Frame дописывает в dst кадр с телом body (результат Begin + поля) и
// возвращает dst. Буфер body переиспользуется следующим Begin.
func (w *PacketWriter) Frame(dst, body []byte) []byte {
	w.body = body[:0]
	if w.threshold < 0 {
		dst = AppendVarInt(dst, int32(len(body)))
		return append(dst, body...)
	}
	if len(body) < w.threshold {
		// Сжатый режим, но пакет короткий: data length = 0.
		dst = AppendVarInt(dst, int32(len(body)+1))
		dst = append(dst, 0)
		return append(dst, body...)
	}
	w.zbuf.Reset()
	if w.zw == nil {
		w.zw, _ = zlib.NewWriterLevel(&w.zbuf, zlib.BestSpeed)
	} else {
		w.zw.Reset(&w.zbuf)
	}
	_, _ = w.zw.Write(body) // запись в bytes.Buffer не ошибается
	_ = w.zw.Close()
	n := VarIntSize(int32(len(body))) + w.zbuf.Len()
	dst = AppendVarInt(dst, int32(n))
	dst = AppendVarInt(dst, int32(len(body)))
	return append(dst, w.zbuf.Bytes()...)
}
