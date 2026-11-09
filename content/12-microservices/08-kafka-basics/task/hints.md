## Подсказка 1

`NewRecord` — просто сборка `*kgo.Record`: `Key: []byte(e.OrderID)`, `Value` из `json.Marshal(e)`, `Headers: []kgo.RecordHeader{{Key: HeaderEventType, Value: []byte(e.Type)}, ...}`. Поле `Partition` не трогайте: при отправке его выберет партиционер по ключу.

## Подсказка 2

`Publish`: сначала постройте все записи через `NewRecord` (первая же ошибка — выход, ничего не отправлено), потом одним вызовом `cl.ProduceSync(ctx, recs...)`. Ошибку берите из `results.FirstErr()`. Не сопоставляйте `results[i]` с `events[i]`: результаты идут в порядке подтверждений. Клиент заполняет `Partition` и `Offset` в тех самых `*kgo.Record`, которые вы передали, — `recs[i]` и есть позиция `events[i]`.

## Подсказка 3

`Histories`: скопируйте срез и отсортируйте по `Offset` (`slices.SortStableFunc` с `cmp.Compare(a.Offset, b.Offset)`) — для записей одной партиции это и есть порядок. Затем один проход: карта `ключ → партиция` (встретили ключ в другой партиции — `ErrSplitKey`), `json.Unmarshal(r.Value, &e)` и `out[key] = append(out[key], e.Type)`. Начните с `out := map[string][]string{}` — тогда пустой вход даст пустую карту, а не `nil`.
