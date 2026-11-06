## Подсказка 1

Ключ контекста — пустая структура своего типа: `type requestIDKey struct{}`. Значение кладёт `context.WithValue(ctx, requestIDKey{}, id)`, достаёт `ctx.Value(requestIDKey{}).(string)` — с проверкой `, ok` или без паники через `id, _ := ...`.

## Подсказка 2

Обработчик — структура с полем `next slog.Handler` и четырьмя методами интерфейса `slog.Handler`. `Enabled` просто спрашивает `next`. В `Handle` добавьте атрибут через `r.AddAttrs(slog.String("request_id", id))` и вызовите `next.Handle`. Перед изменением записи сделайте `r = r.Clone()`.

## Подсказка 3

```go
func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name)}
}
```

Если вернуть `h.next.WithAttrs(attrs)` без обёртки, логгер после `With` потеряет ваш обработчик, и `request_id` пропадёт.
