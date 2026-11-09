## Подсказка 1

Понадобятся свои `appendVarInt` и `readVarInt` (как в уроке 01: цикл до 5 байт, `io.EOF` на первом байте, `io.ErrUnexpectedEOF` дальше). В `AppendCompressed` сначала соберите тело целиком — `appendVarInt(nil, 0)` + пакет или `appendVarInt(nil, len)` + zlib, — потом проверьте `len(body) > MaxPacketLength` и только после этого дописывайте к `dst` длину и тело. Для zlib: `zw := zlib.NewWriter(&buf)`, `zw.Write(packet)`, `zw.Close()` (без `Close` поток не дописан).

## Подсказка 2

В `ReadCompressed` прочитайте длину, проверьте диапазон, затем `make([]byte, n)` и `io.ReadFull(r, body)`. Дальше работайте только с телом: `br := bytes.NewReader(body)`, `readVarInt(br)` — длина данных. Обрыв внутри неё (`io.ErrUnexpectedEOF`) превратите в `ErrBadLength`. Для длины `0` пакет — `body[len(body)-br.Len():]`.

## Подсказка 3

Распаковка с защитой от бомбы:

```go
zr, err := zlib.NewReader(br) // ошибка → ErrBadCompression
var out bytes.Buffer
_, err = io.Copy(&out, io.LimitReader(zr, int64(dataLen)+1)) // ошибка → ErrBadCompression
if out.Len() != int(dataLen) { /* ErrSizeMismatch */ }
```

Порядок проверок до распаковки: `< 0`, `< threshold`, `> maxData`.
