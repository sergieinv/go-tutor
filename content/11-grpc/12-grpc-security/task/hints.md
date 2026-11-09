## Подсказка 1

Разделите проверку на три шага и выходите на первом же провале: кто на том конце соединения (TLS), чей это токен (metadata), можно ли ему этот метод (политика). Личность берите только из `peer.FromContext(ctx)` → `credentials.TLSInfo` → `State.VerifiedChains`: поле `PeerCertificates` заполняется и тогда, когда сервер цепочку не проверял. Caller кладите в контекст через `context.WithValue` с неэкспортируемым ключом-типом.

## Подсказка 2

- Токен: `metadata.FromIncomingContext(ctx)`, затем `md.Get("authorization")` — это срез; ровно одно значение, `strings.CutPrefix(v, "Bearer ")`, пустой остаток — отказ.
- Поиск токена: не `p.Tokens[tok]`, а перебор всех ключей с `subtle.ConstantTimeCompare([]byte(tok), []byte(key)) == 1`.
- Найденный `Grant.Service` должен совпасть с CN сертификата, иначе `Unauthenticated`.
- Права: метода нет в `Methods` или список ролей пуст — `PermissionDenied`; иначе ищите пересечение ролей метода и ролей токена (`slices.Contains`).
- Сообщения ошибок — постоянные строки вроде `"unauthenticated"`, без присланных значений.

## Подсказка 3

```go
func tlsIdentity(ctx context.Context) (string, bool) {
	pr, ok := peer.FromContext(ctx)
	if !ok {
		return "", false
	}
	ti, ok := pr.AuthInfo.(credentials.TLSInfo)
	if !ok || len(ti.State.VerifiedChains) == 0 || len(ti.State.VerifiedChains[0]) == 0 {
		return "", false
	}
	cn := ti.State.VerifiedChains[0][0].Subject.CommonName
	return cn, cn != ""
}
```

В interceptor: публичный метод (`slices.Contains(p.Public, info.FullMethod)`) — сразу `handler(ctx, req)`; затем `tlsIdentity`, токен, сравнение сервиса, роли; в конце — `handler(context.WithValue(ctx, callerKey{}, Caller{...}), req)`.
