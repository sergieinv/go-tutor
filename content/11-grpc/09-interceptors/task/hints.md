## Подсказка 1

Каждое звено — функция `func(ctx, req, info, handler) (any, error)`, которая решает, звать ли `handler`. Порядок в срезе = порядок обёрток: первое звено — внешнее. Отсюда требования: logging должен видеть request id (значит, request id снаружи) и итоговый код после recovery и auth (значит, logging снаружи них), а recovery — перехватить панику обработчика и auth.

## Подсказка 2

- request id: `metadata.ValueFromIncomingContext(ctx, "x-request-id")`, пустое значение — как отсутствие; ответный заголовок — `grpc.SetHeader(ctx, metadata.Pairs("x-request-id", rid))`. В контекст — `context.WithValue` с ключом своего неэкспортируемого типа.
- logging: вызвать `handler`, потом записать строку со `status.Code(err)` (для `nil` это `OK`, для обычной ошибки — `Unknown`).
- recovery: `defer` + `recover()` и **именованные** результаты, иначе из `defer` ошибку не вернуть.
- auth: ровно одно значение `authorization`, `strings.CutPrefix(v, "Bearer ")`, непустой токен, сравнение `subtle.ConstantTimeCompare` с токенами всех пользователей.

## Подсказка 3

```go
func recovery(log Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if p := recover(); p != nil {
				log(fmt.Sprintf("panic %s rid=%s: %v", info.FullMethod, RequestIDFrom(ctx), p))
				resp, err = nil, status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

func Chain(cfg Config) []grpc.UnaryServerInterceptor {
	return []grpc.UnaryServerInterceptor{requestID(cfg.NewID), logging(cfg.Log), recovery(cfg.Log), auth(cfg.Tokens)}
}
```
