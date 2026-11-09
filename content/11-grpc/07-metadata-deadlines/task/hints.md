## Подсказка 1

Всё, что нужно, уже лежит во входящем `ctx`: metadata (`metadata.FromIncomingContext`), дедлайн (`ctx.Deadline()`) и отмена. Значит, вызов Stock должен получить контекст, **производный** от входящего, а не `context.Background()`: тогда дедлайн и отмена дойдут до Stock сами. Пробрасывать вручную нужно только metadata — входящая и исходящая хранятся в контексте раздельно.

## Подсказка 2

Порядок шагов: id запроса (`md.Get("x-request-id")`, пусто — `cfg.NewID()`) → `grpc.SetHeader(ctx, metadata.Pairs("x-request-id", id))`, ошибку которого можно игнорировать → проверка полей → дедлайн. Остаток считайте по часам из конфигурации: `dl.Sub(cfg.Now())`, а не `time.Until(dl)`. Нет дедлайна — `context.WithTimeout(ctx, cfg.DefaultTimeout)` и `defer cancel()`. Не `WithDeadline(ctx, cfg.Now().Add(...))`: контекст отсчитывает время по настоящим часам, а внедрённые часы в тестах могут показывать другое время — они нужны только для сравнения остатка с `MinBudget`.

## Подсказка 3

```go
out := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-request-id", id))
res, err := s.stock.Reserve(out, &ReserveRequest{
	OrderId: req.GetOrderId(), Sku: req.GetSku(), Quantity: req.GetQuantity(),
})
if err != nil {
	return nil, err // статус Stock — как есть
}
```

`metadata.NewOutgoingContext(ctx, incomingMD)` был бы ошибкой: вместе с id запроса уйдут `authorization` и всё остальное, что клиент прислал Checkout.
