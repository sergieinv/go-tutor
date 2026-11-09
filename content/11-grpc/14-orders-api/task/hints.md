## Подсказка 1

Разложите задачу на части и делайте по очереди: хранилище под одним `sync.Mutex` (срез заказов в порядке создания + map по id), четыре метода с кодами ошибок, interceptor валидации, затем идемпотентность и последней — gateway. Валидацию не пишите руками: правила уже в контракте, их проверяет `protovalidate.New()` + `Validate(req)` в unary interceptor, который вы передаёте в `grpc.NewServer`. Gateway ходит в сервер через `conn`, так что тот же interceptor проверит и REST.

## Подсказка 2

- `*protovalidate.ValidationError` (через `errors.As`) → `errdetails.BadRequest`: для каждого `v` из `Violations` — `Field: protovalidate.FieldPathString(v.Proto.GetField())`, `Description: v.Proto.GetMessage()`; статус — `status.New(codes.InvalidArgument, ...).WithDetails(br)`.
- Ключ: `metadata.FromIncomingContext(ctx)` → `md.Get("idempotency-key")`. Храните `map[ключ]{копия запроса, id заказа}` и сравнивайте `proto.Equal`. Проверка ключа и создание — под одной блокировкой, иначе два конкурентных повтора создадут два заказа.
- Номер заказа — `len(orders)+1` в момент создания: отказы его не расходуют.
- Токен страницы — закодированное смещение (например, `base64.RawURLEncoding` от `"off:12"`); всё, что не декодируется, — `InvalidArgument`.
- Gateway по умолчанию пропускает в metadata только заголовки с префиксом `Grpc-Metadata-`. Нужен `runtime.WithIncomingHeaderMatcher`.

## Подсказка 3

```go
func NewGateway(ctx context.Context, conn *grpc.ClientConn) (http.Handler, error) {
	mux := runtime.NewServeMux(runtime.WithIncomingHeaderMatcher(func(h string) (string, bool) {
		if strings.EqualFold(h, "idempotency-key") {
			return "idempotency-key", true
		}
		return runtime.DefaultHeaderMatcher(h)
	}))
	if err := RegisterOrderServiceHandler(ctx, mux, conn); err != nil {
		return nil, err
	}
	return mux, nil
}
```

`ListOrders`: соберите заказы клиента в порядке создания, `size := page_size` (0 → 10), страница — `mine[offset:min(offset+size, len(mine))]`, `next_page_token` — только если после конца страницы ещё есть заказы.
