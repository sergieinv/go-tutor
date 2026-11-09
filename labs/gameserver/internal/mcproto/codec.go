// Пакет mcproto — примитивы протокола Minecraft Java Edition: VarInt,
// строки, координаты, углы, кадрирование и сжатие пакетов.
//
// Кодирование — в стиле append: каждая функция дописывает значение в
// срез и возвращает его. Так один буфер переиспользуется тик за тиком, и
// горячий путь не аллоцирует. Декодирование — Decoder с «липкой» ошибкой:
// поля читаются подряд, ошибку проверяют один раз в конце.
//
// Формат описан в minecraft.wiki «Java Edition protocol» (протокол 767,
// Minecraft 1.21 / 1.21.1).
package mcproto

import (
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// Ошибки декодирования. Это ошибки клиента: соединение закрывается.
var (
	ErrVarIntTooBig = errors.New("mcproto: varint is too big")
	ErrShort        = errors.New("mcproto: packet is too short")
	ErrStringTooBig = errors.New("mcproto: string is too long")
	ErrBadUTF8      = errors.New("mcproto: string is not valid UTF-8")
	ErrTrailing     = errors.New("mcproto: trailing bytes in packet")
)

// AppendVarInt дописывает v в формате VarInt: по 7 бит, младшие вперёд,
// старший бит байта — «дальше есть ещё». Отрицательные числа занимают 5 байт.
func AppendVarInt(dst []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		dst = append(dst, byte(u)|0x80)
		u >>= 7
	}
	return append(dst, byte(u))
}

// AppendVarLong — то же для int64 (до 10 байт).
func AppendVarLong(dst []byte, v int64) []byte {
	u := uint64(v)
	for u >= 0x80 {
		dst = append(dst, byte(u)|0x80)
		u >>= 7
	}
	return append(dst, byte(u))
}

// VarIntSize — сколько байт займёт v.
func VarIntSize(v int32) int {
	u := uint32(v)
	n := 1
	for u >= 0x80 {
		u >>= 7
		n++
	}
	return n
}

// AppendString дописывает строку: VarInt-длина в байтах UTF-8 и байты.
func AppendString(dst []byte, s string) []byte {
	dst = AppendVarInt(dst, int32(len(s)))
	return append(dst, s...)
}

func AppendBool(dst []byte, v bool) []byte {
	if v {
		return append(dst, 1)
	}
	return append(dst, 0)
}

func AppendShort(dst []byte, v int16) []byte { return binary.BigEndian.AppendUint16(dst, uint16(v)) }
func AppendInt(dst []byte, v int32) []byte   { return binary.BigEndian.AppendUint32(dst, uint32(v)) }
func AppendLong(dst []byte, v int64) []byte  { return binary.BigEndian.AppendUint64(dst, uint64(v)) }
func AppendFloat(dst []byte, v float32) []byte {
	return binary.BigEndian.AppendUint32(dst, math.Float32bits(v))
}
func AppendDouble(dst []byte, v float64) []byte {
	return binary.BigEndian.AppendUint64(dst, math.Float64bits(v))
}

// AppendUUID дописывает UUID — 16 байт, как есть.
func AppendUUID(dst []byte, u [16]byte) []byte { return append(dst, u[:]...) }

// AppendAngle — угол в 1/256 оборота (так передаются поворот и наклон
// сущностей).
func AppendAngle(dst []byte, deg float32) []byte {
	return append(dst, byte(int32(math.Floor(float64(deg)*256/360))))
}

// AppendPosition — координаты блока, упакованные в 64 бита:
// x (26 бит) | z (26 бит) | y (12 бит).
func AppendPosition(dst []byte, x, y, z int32) []byte {
	v := (uint64(x)&0x3FFFFFF)<<38 | (uint64(z)&0x3FFFFFF)<<12 | uint64(y)&0xFFF
	return binary.BigEndian.AppendUint64(dst, v)
}

// DecodePosition — обратное к AppendPosition (с восстановлением знака).
func DecodePosition(v uint64) (x, y, z int32) {
	x = int32(int64(v) >> 38)
	y = int32(int64(v<<52) >> 52)
	z = int32(int64(v<<26) >> 38)
	return x, y, z
}

// Decoder читает поля пакета из среза. Первая ошибка «залипает»: все
// следующие чтения возвращают нули, а Err() — эту ошибку.
type Decoder struct {
	b   []byte
	err error
}

// NewDecoder — декодер полезной нагрузки пакета (без id).
func NewDecoder(b []byte) Decoder { return Decoder{b: b} }

// Err — первая ошибка чтения.
func (d *Decoder) Err() error { return d.err }

// Len — сколько байт осталось.
func (d *Decoder) Len() int { return len(d.b) }

// Finish проверяет, что пакет прочитан целиком: лишние байты — признак
// чужой версии протокола или подделки.
func (d *Decoder) Finish() error {
	if d.err == nil && len(d.b) != 0 {
		d.err = ErrTrailing
	}
	return d.err
}

func (d *Decoder) fail(err error) {
	if d.err == nil {
		d.err = err
	}
	d.b = nil
}

func (d *Decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if n < 0 || len(d.b) < n {
		d.fail(ErrShort)
		return nil
	}
	v := d.b[:n]
	d.b = d.b[n:]
	return v
}

// VarInt читает VarInt (не длиннее 5 байт).
func (d *Decoder) VarInt() int32 {
	if d.err != nil {
		return 0
	}
	var u uint32
	for i := 0; i < 5; i++ {
		if len(d.b) == 0 {
			d.fail(ErrShort)
			return 0
		}
		c := d.b[0]
		d.b = d.b[1:]
		u |= uint32(c&0x7F) << (7 * i)
		if c&0x80 == 0 {
			return int32(u)
		}
	}
	d.fail(ErrVarIntTooBig)
	return 0
}

// String читает строку не длиннее maxChars символов (как в протоколе:
// лимит — в символах, байтов не больше maxChars*3).
func (d *Decoder) String(maxChars int) string {
	n := d.VarInt()
	if d.err != nil {
		return ""
	}
	if n < 0 || int(n) > maxChars*3 {
		d.fail(ErrStringTooBig)
		return ""
	}
	b := d.take(int(n))
	if d.err != nil {
		return ""
	}
	if !utf8.Valid(b) {
		d.fail(ErrBadUTF8)
		return ""
	}
	if utf8.RuneCount(b) > maxChars {
		d.fail(ErrStringTooBig)
		return ""
	}
	return string(b)
}

func (d *Decoder) Bool() bool {
	b := d.take(1)
	return b != nil && b[0] != 0
}

func (d *Decoder) Byte() byte {
	b := d.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (d *Decoder) Short() int16 {
	b := d.take(2)
	if b == nil {
		return 0
	}
	return int16(binary.BigEndian.Uint16(b))
}

func (d *Decoder) UShort() uint16 { return uint16(d.Short()) }

func (d *Decoder) Int() int32 {
	b := d.take(4)
	if b == nil {
		return 0
	}
	return int32(binary.BigEndian.Uint32(b))
}

func (d *Decoder) Long() int64 {
	b := d.take(8)
	if b == nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(b))
}

func (d *Decoder) Float() float32 {
	b := d.take(4)
	if b == nil {
		return 0
	}
	return math.Float32frombits(binary.BigEndian.Uint32(b))
}

func (d *Decoder) Double() float64 {
	b := d.take(8)
	if b == nil {
		return 0
	}
	return math.Float64frombits(binary.BigEndian.Uint64(b))
}

func (d *Decoder) UUID() (u [16]byte) {
	b := d.take(16)
	copy(u[:], b)
	return u
}

// Position читает упакованные координаты блока.
func (d *Decoder) Position() (x, y, z int32) {
	b := d.take(8)
	if b == nil {
		return 0, 0, 0
	}
	return DecodePosition(binary.BigEndian.Uint64(b))
}

// Skip пропускает n байт.
func (d *Decoder) Skip(n int) { d.take(n) }

// Rest возвращает непрочитанный остаток (без копирования).
func (d *Decoder) Rest() []byte {
	b := d.b
	d.b = nil
	return b
}
