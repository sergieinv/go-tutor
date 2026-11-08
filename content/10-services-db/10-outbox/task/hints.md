## Подсказка 1

`CreateOrder`: проверка входа — до базы. Затем `tx, err := db.BeginTx(ctx, nil)`, сразу `defer tx.Rollback()`, и **оба** `INSERT` — через `tx.ExecContext`: id заказа — `res.LastInsertId()`, тело события — `json.Marshal(OrderCreated{...})`, ключ — `strconv.FormatInt(id, 10)`. В конце `tx.Commit()`. Запрос через `db` внутри транзакции уйдёт в другое соединение — мимо транзакции, а с пулом из одного соединения повиснет.

## Подсказка 2

`RunOnce` делится на два шага. Сначала прочитать пачку целиком: `SELECT id, topic, key, payload FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT ?`, всё в `[]Event`, `rows.Close()` и `rows.Err()`. Только потом публиковать — публикатор (и следующий `UPDATE`) получат свободное соединение.

## Подсказка 3

Цикл по пачке:

```go
for i, e := range batch {
	if err := r.Pub.Publish(ctx, e); err != nil {
		return i, fmt.Errorf("публикация события %d: %w", e.ID, err) // остальные не трогаем
	}
	if _, err := r.DB.ExecContext(ctx, `UPDATE outbox SET published_at = CURRENT_TIMESTAMP WHERE id = ?`, e.ID); err != nil {
		return i, fmt.Errorf("отметка события %d: %w", e.ID, err)
	}
}
return len(batch), nil
```

Событие помечается только после успешной публикации — поэтому сбой брокера его не теряет, а сбой отметки приводит к повторной публикации (at-least-once).
