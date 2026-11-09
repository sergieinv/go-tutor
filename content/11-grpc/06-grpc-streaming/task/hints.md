## Подсказка 1

Храните события в одном срезе под мьютексом: событие с номером `seq` лежит в `events[seq-1]`. Тогда подписчику не нужен свой канал с событиями — достаточно номера следующего события, которое он ещё не отправил. Издатель только дописывает в срез и потому никогда не ждёт подписчиков, а медленный подписчик отстаёт, ничего не теряя.

## Подсказка 2

Подписчику, который всё отправил, нужно уснуть до следующей публикации **или** до отмены вызова. `sync.Cond` не умеет ждать вместе с `ctx.Done()` — нужен канал: храните `changed chan struct{}`, при каждой публикации закрывайте его и создавайте новый. Закрытие канала будит всех ждущих сразу. Подписчик под мьютексом берёт копию новых событий и текущий `changed`, отпускает мьютекс, отправляет события и, если отправлять было нечего, делает `select` по `changed` и `stream.Context().Done()`.

## Подсказка 3

```go
next := max(req.GetFromSeq(), 1)
for {
	batch, changed := s.since(next) // копия events[next-1:] и текущий канал
	for _, e := range batch {
		if err := stream.Send(&SubscribeResponse{Seq: next, OrderId: e.orderID, Status: e.status}); err != nil {
			return err
		}
		next++
	}
	if len(batch) > 0 {
		continue
	}
	select {
	case <-stream.Context().Done():
		return status.FromContextError(stream.Context().Err()).Err()
	case <-changed:
	}
}
```

В `Publish` читайте `Recv` до `io.EOF` и только тогда отвечайте `SendAndClose`; невалидное событие — сразу `return status.Error(codes.InvalidArgument, ...)`.
