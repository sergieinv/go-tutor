## Подсказка 1

Запись: проверьте `len(payload) > max`, затем соберите один буфер `make([]byte, 4+len(payload))`, положите длину через `binary.BigEndian.PutUint32(buf, uint32(len(payload)))` и скопируйте данные в `buf[4:]`. Ошибку размера оборачивайте через `%w`, чтобы работал `errors.Is`.

## Подсказка 2

Чтение заголовка — `io.ReadFull(r, hdr[:])` в массив `[4]byte`. `ReadFull` уже возвращает ровно то, что нужно: `io.EOF`, если не прочитано ни байта, и `io.ErrUnexpectedEOF`, если поток оборвался посередине. Не оборачивайте `r` в `bufio` — он прочитает впрок следующий кадр.

## Подсказка 3

Длину сравните с `max` до `make`. Данные читайте тоже через `io.ReadFull`; если он вернул `io.EOF` (заголовок был, а данных нет вовсе), замените её на `io.ErrUnexpectedEOF`:

```go
payload := make([]byte, n)
if _, err := io.ReadFull(r, payload); err != nil {
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return nil, err
}
```
