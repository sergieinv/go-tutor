// Кадрирование учебного протокола игрового сервера — того же вида,
// что у Minecraft: `VarInt длина` → `VarInt id пакета` → данные.
// TCP отдаёт поток байт, и где кончается один пакет, знает только
// этот префикс длины. Пример — пакет `frame` с тестами: нажмите
// «Запустить тесты», затем обе кнопки «Fuzzing».
package frame

import (
	"bufio"
	"errors"
	"io"
)

// Лимиты — часть протокола, а не «на всякий случай». VarInt для
// int32 занимает не больше 5 байт; кадр — не больше `MaxFrame`
// (у Minecraft — 2097151, всё, что влезает в 3 байта VarInt).
const (
	MaxVarIntLen = 5
	MaxFrame     = 1 << 16
)

var (
	ErrVarIntTooLong      = errors.New("varint: длиннее 5 байт")
	ErrVarIntOverflow     = errors.New("varint: не помещается в 32 бита")
	ErrVarIntNonCanonical = errors.New("varint: лишний нулевой байт")
	ErrFrameTooLarge      = errors.New("frame: длина больше лимита")
	ErrBadLength          = errors.New("frame: неверная длина")
)

// VarInt: по 7 бит на байт, младшие группы первыми, старший бит
// байта — «дальше есть ещё». int32 кодируется как uint32, поэтому
// отрицательные числа всегда занимают 5 байт.
func AppendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

// Декодер читает по байту через `io.ByteReader` (его умеют
// `bufio.Reader` и `bytes.Reader`) и не верит входу: шестой байт,
// лишние биты в пятом и «неканонические» формы вроде `80 00`
// (ноль в двух байтах) — ошибки. Строгость даёт свойство: у каждого
// числа ровно одна запись, его и проверяет fuzz-тест.
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
	panic("недостижимо")
}

// Чтение кадра: длина, проверка, и только потом память. Порядок
// важен: `make([]byte, n)` до проверки отдаёт размер буфера
// в руки клиента. `io.ReadFull` дочитывает тело, сколько бы
// кусков ни понадобилось, а обрыв посередине превращает
// в `io.ErrUnexpectedEOF`.
func ReadFrame(r *bufio.Reader, max int) ([]byte, error) {
	n, err := ReadVarInt(r)
	if err != nil {
		return nil, err
	}
	if n < 1 {
		return nil, ErrBadLength
	}
	if int(n) > max {
		return nil, ErrFrameTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return body, nil
}

// Запись — зеркально: длина тела и тело. Тело кадра — это
// VarInt id и данные пакета; их собирают в один срез заранее,
// чтобы знать длину.
func AppendFrame(b, body []byte) []byte {
	b = AppendVarInt(b, int32(len(body)))
	return append(b, body...)
}
