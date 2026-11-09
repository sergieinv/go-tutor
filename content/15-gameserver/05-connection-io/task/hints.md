## Подсказка 1

Нужны три канала: очередь `chan []byte` с буфером `queue`, `quit` — закрывается в `Close`, `done` — закрывается писателем при выходе. Канал очереди не закрывайте никогда: `Send` может выполняться одновременно с `Close`, а отправка в закрытый канал паникует даже внутри `select` с `default`. Причину храните под `sync.Mutex` — по ней же `Close` понимает, что он не первый.

## Подсказка 2

`Send`: сначала неблокирующая проверка `quit` (закрыт — `false`), потом `select { case c.queue <- msg: return true; default: c.Close(ErrSlowClient); return false }`. `Close`: под мьютексом — если причина уже есть, выйти; иначе запомнить причину (`nil` → `ErrClosed`) и закрыть `quit`; после мьютекса — `conn.Close()`. Ждать писателя в `Close` нельзя: его зовут из тика.

## Подсказка 3

Писатель:

```go
defer close(c.done)
defer func() {
	if c.onClose != nil {
		c.onClose(c.Err())
	}
}()
for {
	select {
	case <-c.quit:
		return
	case msg := <-c.queue:
		// select выбирает случайно: ещё раз проверьте quit без ожидания
		if _, err := c.conn.Write(msg); err != nil {
			c.Close(err)
			return
		}
	}
}
```
