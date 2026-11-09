## Подсказка 1

`ListOrders` не знает, по какому маршруту пришёл запрос: gateway кладёт `customer_id` из пути или из query в одно и то же поле. Достаточно отфильтровать и отсортировать.

Всё остальное — опции `runtime.NewServeMux`: `WithErrorHandler`, `WithRoutingErrorHandler`, `WithIncomingHeaderMatcher`, `WithMarshalerOption`. Регистрация в процессе — `RegisterOrderServiceHandlerServer`, лимит тела — своя обёртка вокруг mux, которая до маршрутизации читает тело целиком через `http.MaxBytesReader`: декодер gateway читает только первое JSON-значение, и хвост из пробелов иначе пройдёт мимо лимита.

## Подсказка 2

- Имя кода в верхнем регистре уже есть в `google.golang.org/genproto/googleapis/rpc/code`: `code.Code(st.Code()).String()` вернёт `"NOT_FOUND"`.
- HTTP-статус — `runtime.HTTPStatusFromCode`. Если ошибка — `*runtime.HTTPStatusError` (`errors.As`), статус берётся из её поля `HTTPStatus`, а код и текст — из `Err`.
- Роутер gateway сообщает о «нет пути» и «не тот метод» через `RoutingErrorHandlerFunc` с HTTP-статусом. Оберните его в `HTTPStatusError` и вызовите свой `ErrorHandler`, иначе 405 превратится в `Unimplemented` → 501.
- Заголовки в matcher приходят в каноническом виде: `X-Request-Id`.

## Подсказка 3

```go
func routingError(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, httpStatus int) {
	c := codes.Internal
	switch httpStatus {
	case http.StatusNotFound:
		c = codes.NotFound
	case http.StatusMethodNotAllowed:
		c = codes.Unimplemented
	case http.StatusBadRequest:
		c = codes.InvalidArgument
	}
	ErrorHandler(ctx, mux, m, w, r, &runtime.HTTPStatusError{HTTPStatus: httpStatus, Err: status.Error(c, http.StatusText(httpStatus))})
}

mux := runtime.NewServeMux(
	runtime.WithErrorHandler(ErrorHandler),
	runtime.WithRoutingErrorHandler(routingError),
	runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
		if key == "X-Request-Id" {
			return "x-request-id", true
		}
		return runtime.DefaultHeaderMatcher(key)
	}),
	runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
		MarshalOptions: protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true},
	}),
)
```
