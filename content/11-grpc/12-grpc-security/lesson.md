---
title: Безопасность gRPC
summary: "TLS и mTLS со своим CA (сертификаты в памяти), личность клиента из peer и credentials.TLSInfo, токены в metadata через PerRPCCredentials и RequireTransportSecurity, auth-interceptor с ролями по методам, Unauthenticated против PermissionDenied, ловушки InsecureSkipVerify и непроверенных сертификатов."
difficulty: 4
minutes: 45
runner:
  race: true
prerequisites:
  - 11-grpc/09-interceptors
  - 11-grpc/07-metadata-deadlines
  - 09-security/08-crypto
  - 09-security/07-passwords-tokens
docs:
  - title: "grpc-go: credentials"
    url: https://pkg.go.dev/google.golang.org/grpc/credentials
  - title: "gRPC: Authentication"
    url: https://grpc.io/docs/guides/auth/
  - title: "crypto/tls: Config"
    url: https://pkg.go.dev/crypto/tls#Config
---

До сих пор клиент подключался с `insecure.NewCredentials()`: трафик шёл открытым текстом, а сервер принимал любого. Для loopback в песочнице это допустимо, для сети между сервисами — нет. Безопасность gRPC собирается из трёх слоёв, и каждый закрывает своё:

1. **TLS** — шифрование и подлинность сервера: клиент проверяет, что на том конце сервер с сертификатом от доверенного CA и с нужным именем.
2. **mTLS** — то же в обратную сторону: клиент предъявляет свой сертификат, сервер проверяет его по своему CA. После этого сервер знает, *какой сервис* к нему пришёл, — это личность уровня соединения.
3. **Токен в metadata** — кто действует и с какими правами: пользователь, сервисный аккаунт, роли. Проверяет его auth-interceptor, он же решает, можно ли вызывать метод.

В grpc-go транспорт настраивают через `credentials.TransportCredentials`. `credentials.NewTLS(cfg)` принимает обычный `*tls.Config` и добавляет то, что нужно HTTP/2: ALPN `h2` (с grpc-go 1.67 без него соединение отвергается) и минимум TLS 1.2. Сервер — `grpc.NewServer(grpc.Creds(creds))`, клиент — `grpc.NewClient(addr, grpc.WithTransportCredentials(creds))`. Всё остальное — знакомый по уроку `09-security/08-crypto` `crypto/tls`: `Certificates` (свой сертификат и ключ), `RootCAs` у клиента (кому доверять как серверу), `ClientCAs` и `ClientAuth` у сервера (кому доверять как клиенту). Для файлов есть короткие `credentials.NewServerTLSFromFile` и `NewClientTLSFromFile`, но mTLS через них не настроить — нужен `NewTLS`.

**Личность клиента** обработчик достаёт из контекста: `peer.FromContext(ctx)` возвращает `*peer.Peer`, его `AuthInfo` для TLS-соединения — `credentials.TLSInfo` с полем `State tls.ConnectionState`. Брать имя нужно из `State.VerifiedChains[0][0]` — листового сертификата цепочки, которую проверил ваш CA. В примере имя — `CommonName`; в service mesh и SPIFFE личность лежит в URI SAN вида `spiffe://cluster.local/ns/shop/sa/billing`, grpc-go разбирает её в `TLSInfo.SPIFFEID` (поле экспериментальное).

**Токен** клиент прикладывает к каждому вызову через `credentials.PerRPCCredentials` — интерфейс из двух методов: `GetRequestMetadata` возвращает заголовки (`authorization: Bearer …`), а `RequireTransportSecurity` говорит, можно ли слать их без TLS. Регистрируют его на соединение (`grpc.WithPerRPCCredentials`) или на один вызов (`grpc.PerRPCCredentials(...)` как `CallOption`). Готовые реализации — в `credentials/oauth` (OAuth2 и JWT для Google API); для своих токенов хватает пяти строк, как в примере. Сервер читает заголовок в interceptor, сравнивает токен за постоянное время (`subtle.ConstantTimeCompare`, урок `09-security/07-passwords-tokens`) и проверяет права метода по `info.FullMethod`.

Пример поднимает сервер с mTLS на loopback и прогоняет шесть сценариев: правильный клиент, недостаточная роль, украденный токен с чужим сертификатом, клиент без сертификата, сертификат чужого CA и попытка отправить токен по каналу без TLS. Сертификаты выпускаются в памяти при каждом запуске: ECDSA P-256, срок — час. Токены — `crypto/rand.Text()`, в вывод они не попадают.

**Секреты и лимиты.** Ключи и токены не хранят в коде и образе: сервис читает их из переменных окружения или файлов, смонтированных из Secret, и никогда не логирует (урок `09-security/11-secrets-logs-errors`). Сертификаты с коротким сроком ротируют без перезапуска: `tls.Config.GetCertificate` (сервер) и `GetClientCertificate` (клиент) вызываются на каждое рукопожатие и могут отдавать свежий сертификат. Ещё одна граница доверия — размер сообщения: сервер по умолчанию принимает до 4 МиБ (`grpc.MaxRecvMsgSize`), клиент — тоже до 4 МиБ (`grpc.MaxCallRecvMsgSize`). Лимит стоит выставить явно под свой контракт, чтобы один запрос не съел память; превышение вернёт `codes.ResourceExhausted`.

## Как это в C#

- **Где настраивается TLS.** В ASP.NET Core TLS и клиентские сертификаты — дело Kestrel (`ConfigureHttpsDefaults`, `ClientCertificateMode.RequireCertificate`, `ClientCertificateValidation`), а gRPC о них не знает. В Go транспорт и gRPC сводятся в одном месте: `tls.Config` → `credentials.NewTLS` → `grpc.Creds`.
- **Аутентификация и авторизация.** В .NET это конвейер: `AddAuthentication().AddCertificate()` или `AddJwtBearer()`, затем `[Authorize(Roles = "admin")]` на методе сервиса, а `context.GetHttpContext().User` уже заполнен. В Go конвейера нет: всё делает ваш interceptor — достаёт личность из `peer`, токен из metadata, права по `info.FullMethod`.
- **Токен на клиенте.** `CallCredentials.FromInterceptor` + `ChannelCredentials.Create(new SslCredentials(), callCreds)` — аналог `PerRPCCredentials`. И там и там токен по незащищённому каналу не уйдёт: `GrpcChannel` требует `UnsafeUseInsecureChannelCallCredentials = true`, grpc-go — `RequireTransportSecurity() == false`.
- **Сравнение секретов.** `CryptographicOperations.FixedTimeEquals` ↔ `subtle.ConstantTimeCompare`.

## Ловушки

### `InsecureSkipVerify` «чтобы заработало»

```go
// УЯЗВИМО: клиент не проверяет сертификат сервера — любой посредник
// в сети предъявит свой и прочитает трафик вместе с токенами.
creds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
```

Ошибка `x509: certificate signed by unknown authority` значит, что клиент не знает вашего внутреннего CA, а не что проверку пора выключить. Правильно — передать CA явно: `RootCAs: pool`, где `pool` собран из `ca.crt` через `x509.NewCertPool()` и `AppendCertsFromPEM`. Если имя в сертификате не совпадает с адресом (подключаетесь по IP, а в SAN — DNS-имя), задайте `ServerName` в `tls.Config`, а не отключайте проверку.

### Сертификат клиента есть, но не проверен

```go
// УЯЗВИМО: RequireAnyClientCert требует сертификат, но не проверяет цепочку,
// а PeerCertificates — то, что клиент заявил о себе сам.
cfg := &tls.Config{Certificates: certs, ClientAuth: tls.RequireAnyClientCert}
// ... в interceptor:
name := ti.State.PeerCertificates[0].Subject.CommonName
```

Любой выпустит себе самоподписанный сертификат с CN `billing`. Нужны `ClientAuth: tls.RequireAndVerifyClientCert` и `ClientCAs: pool`, а имя — из `ti.State.VerifiedChains[0][0]`: цепочка там есть, только если её подтвердил ваш CA. Проверка по `VerifiedChains` в interceptor страхует и от ошибки в настройке сервера — задача урока проверяет именно это.

### Unary interceptor не защищает стримы

```go
// УЯЗВИМО: стриминговые методы (ListOrders, Watch) идут мимо проверки.
srv := grpc.NewServer(grpc.Creds(creds), grpc.UnaryInterceptor(auth))
```

`UnaryInterceptor` вызывается только для unary-методов. Для стримов нужен `grpc.StreamInterceptor` (или `ChainStreamInterceptor`) с той же логикой: личность и metadata берутся из `ss.Context()`, а новый контекст с Caller передают обёрткой над `grpc.ServerStream` (урок `11-grpc/09-interceptors`). Регистрируйте оба сразу, а в политике держите правило «метода нет в списке — доступа нет»: новый метод тогда закрыт, пока его явно не откроют.

## Задание

Напишите auth-interceptor сервера, который проверяет mTLS-личность клиента, токен и права роли на метод.

```go
type Grant struct {
	Service string
	Roles   []string
}

type Policy struct {
	Tokens  map[string]Grant    // токен → кому он выдан
	Methods map[string][]string // полное имя метода → роли, любая из которых даёт доступ
	Public  []string            // методы без проверок
}

type Caller struct {
	Service string
	Roles   []string
}

func CallerFrom(ctx context.Context) (Caller, bool)
func UnaryAuth(p Policy) grpc.UnaryServerInterceptor
```

Требования к `UnaryAuth(p)` — проверки идут строго в этом порядке, на первом провале обработчик не вызывается и возвращается ошибка `status` с указанным кодом:

1. Метод из `p.Public` (`info.FullMethod` совпадает буквально) вызывается без проверок, контекст передаётся обработчику как есть (Caller в нём нет).
2. **Личность.** В контексте должен быть `peer` с `AuthInfo` типа `credentials.TLSInfo` — именно значение, как его кладёт grpc-go (любой другой тип, в том числе `*credentials.TLSInfo`, считается соединением без TLS), — и непустой проверенной цепочкой `State.VerifiedChains`, первая цепочка которой тоже непуста (`len(VerifiedChains[0]) > 0`). Имя сервиса — `CommonName` первого сертификата первой цепочки (`VerifiedChains[0][0].Subject.CommonName`); пустое имя, отсутствие `peer`, соединение без TLS или сертификат без проверенной цепочки (только `PeerCertificates`) — `codes.Unauthenticated`.
3. **Токен.** В входящей metadata ровно одно значение `authorization` вида `Bearer <токен>` (схема `Bearer` с заглавной буквы и одним пробелом), токен непустой и буквально совпадает с ключом `p.Tokens`. Иначе (нет заголовка, несколько значений, другая схема, пустой токен, неизвестный токен, его префикс или удлинение) — `codes.Unauthenticated`. Пустой токен отклоняется всегда, даже если в `p.Tokens` есть ключ `""`. Сравнивайте токены за постоянное время.
4. Токен выдан другому сервису (`Grant.Service` не равен имени из сертификата) — `codes.Unauthenticated`.
5. **Права.** Метода нет в `p.Methods`, его список ролей пуст или ни одна роль метода не входит в `Grant.Roles` — `codes.PermissionDenied`.
6. Всё прошло — обработчик вызывается с контекстом, из которого `CallerFrom` возвращает `Caller{Service: имя из сертификата, Roles: роли токена}` и `true`. Делать `Roles` копией (чтобы обработчик не мог поменять политику) — хорошая практика, тесты этого не проверяют.

Сообщения ошибок не должны содержать присланный токен. `CallerFrom` на контексте без Caller возвращает `false`. Тесты вызывают interceptor напрямую и через настоящий mTLS на `bufconn`; часть скрытых тестов — эксплойты: токен без TLS, непроверенный сертификат, украденный токен, метод вне политики, повторные заголовки, утечка токена в ошибку.
