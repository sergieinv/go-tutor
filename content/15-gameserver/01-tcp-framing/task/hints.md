## Подсказка 1

`ReadVarInt` — цикл `for i := range 5`: прочитать байт, добавить `uint32(c&0x7f) << (7*i)`, выйти, если старший бит сброшен. Проверки пятого байта (`c&0x80`, затем `c > 0x0f`) делайте при `i == 4` до выхода из цикла, проверку `c == 0 && i > 0` — в момент выхода. EOF на `i > 0` превратите в `io.ErrUnexpectedEOF`.

## Подсказка 2

В `ReadFrame` сначала длина и обе проверки диапазона, потом `make([]byte, n)` и `io.ReadFull(r, body)` (его `io.EOF` — тоже `io.ErrUnexpectedEOF`). id удобно читать из тела через `bytes.NewReader(body)`: после `ReadVarInt` данные — это `body[len(body)-br.Len():]`. Так `ReadFrame` никогда не читает из `r` больше одного кадра.

## Подсказка 3

Для `DecodePacket` заведите курсор по данным — структуру со срезом `b` и методами `varint()`, `take(n)`, `str(max)`, `float64()`. Каждый метод проверяет, что байтов хватает (`n > len(b)` → `ErrShortPacket`), и сдвигает `b = b[n:]`. В `str` порядок проверок: длина `< 0`, длина `> max`, `take(n)`, `utf8.Valid`. В конце `len(b) != 0` → `ErrTrailingData`. Float — `math.Float64frombits(binary.BigEndian.Uint64(b))`.
