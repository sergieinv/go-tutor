---
title: SSRF
summary: Почему проверка URL строкой бесполезна (userinfo, 0.0.0.0, ::ffff:127.0.0.1, имена), DNS rebinding, защита на уровне сокета через net.Dialer.ControlContext, netip.Addr и его классификаторы, редиректы и схемы.
difficulty: 4
minutes: 40
prerequisites:
  - 08-web/01-http-client
  - 06-stdlib/10-url-parsing
  - 09-security/02-input-validation
docs:
  - title: "Пакет net: Dialer.ControlContext"
    url: https://pkg.go.dev/net#Dialer
  - title: "Пакет net/netip: Addr"
    url: https://pkg.go.dev/net/netip#Addr
  - title: "OWASP SSRF Prevention Cheat Sheet"
    url: https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html
runner:
  race: true
---

Server-Side Request Forgery: сервер по просьбе пользователя делает HTTP-запрос — превью ссылки, вебхук, «импорт по URL», загрузка аватара по адресу, — а пользователь подсовывает адрес, куда сам дотянуться не может. Это админка на `127.0.0.1`, база в частной сети, сервис метаданных облака на `169.254.169.254`, который отдаёт временные ключи доступа. Запрос идёт изнутри периметра, и файрвол его пропускает. В OWASP Top 10:2025 SSRF вошёл в категорию A01 Broken Access Control, в API Security Top 10:2023 это отдельный пункт API7.

Первая идея — проверить URL: распарсить, посмотреть на хост, сравнить со списком. Она не работает. Один и тот же адрес пишется десятком способов: `127.0.0.1`, `localhost`, `[::ffff:127.0.0.1]`, `0.0.0.0` (на Linux и macOS подключение к нему уходит на локальную машину). Некоторые резолверы и клиенты понимают ещё и `127.1`, `0x7f000001`, `2130706433` — в Go это зависит от платформы: cgo-резолвер на macOS их принимает. Любое имя может резолвиться во внутренний адрес: атакующему достаточно своего домена. Разрешённый внешний сервер может ответить редиректом внутрь. А проверка «резолвим заранее, потом запрос» проигрывает **DNS rebinding**: клиент резолвит имя ещё раз, и DNS атакующего с TTL 0 во второй раз отвечает `127.0.0.1`.

Надёжная точка одна — адрес сокета в момент подключения. У `net.Dialer` есть хук `ControlContext(ctx, network, address, rawConn)`. Его вызывают для каждого сокета перед `connect`, а `address` — уже разрешённый `IP:порт`. Ошибка из хука отменяет подключение. Клиент с таким `Dialer` в транспорте проверяет и первый запрос, и каждый редирект, и каждое имя после DNS. Адрес удобно разбирать в `netip.AddrPort`. У `netip.Addr` есть классификаторы `IsLoopback`, `IsPrivate`, `IsLinkLocalUnicast`, `IsUnspecified`, `IsMulticast` и обобщающий `IsGlobalUnicast`, а `Unmap()` сводит `::ffff:a.b.c.d` к IPv4. Остальное — гигиена клиента: только схемы `http`/`https`, лимит редиректов в `CheckRedirect`, таймауты, лимит тела ответа и `Proxy: nil`. С прокси из окружения хук видел бы адрес прокси, а не цели.

## Как это в C#

- В ASP.NET Core защиты от SSRF «из коробки» нет, как и в Go. Ближайший аналог `ControlContext` — `SocketsHttpHandler.ConnectCallback`, но DNS в нём резолвите вы сами. Проверяйте и подключайтесь к одному и тому же списку адресов, иначе получите rebinding.
- `IPAddress.IsLoopback`, `IsIPv6LinkLocal`, `IsIPv4MappedToIPv6`/`MapToIPv4` ≈ методы `netip.Addr`, но готового `IsPrivate` в .NET нет: частные диапазоны перечисляют через `IPNetwork` (.NET 8).
- `MaxAutomaticRedirections` ≈ `CheckRedirect` с `len(via)`. `UseProxy = false` ≈ `Proxy: nil`. `HttpClient.Timeout` по умолчанию 100 с, у `http.DefaultClient` таймаута нет вовсе.

## Ловушки

### Проверили — и отдали URL другому клиенту

```go
// УЯЗВИМО: проверка и запрос резолвят имя независимо (DNS rebinding),
// а http.Get идёт по редиректам без всякой проверки.
ips, _ := net.DefaultResolver.LookupNetIP(ctx, "ip", u.Hostname())
for _, ip := range ips {
	if !isPublic(ip) {
		return errBlocked
	}
}
resp, err := http.Get(u.String())

// Правильно: проверка внутри того клиента, который подключается.
resp, err = safeClient.Get(u.String()) // Dialer.ControlContext в транспорте
```

Проверка, оторванная от подключения, — классическая ошибка TOCTOU (time of check / time of use). Ещё хуже `http.Get` с `http.DefaultClient`: у него нет таймаута, а редиректов он проходит до десяти.

### Сравнение с `==` и самописные диапазоны

```go
// УЯЗВИМО: ::ffff:127.0.0.1 не равен 127.0.0.1 по ==,
// а As4()[0] == 127 паникует на IPv6-адресе.
if a == netip.MustParseAddr("127.0.0.1") || a.As4()[0] == 10 { ... }

// Правильно: Unmap и классификаторы netip.
a = a.Unmap()
ok := a.IsGlobalUnicast() && !a.IsPrivate() && !cgnat.Contains(a)
```

`IsGlobalUnicast` уже исключает loopback, link-local (в том числе `169.254.169.254`), multicast, `0.0.0.0` и `::`. `IsPrivate` — это `10/8`, `172.16/12`, `192.168/16` и `fc00::/7`. Не входит ни туда, ни туда CGNAT `100.64.0.0/10` (бывает внутренней сетью облака) и `0.0.0.0/8`: их исключайте явно. А если вашему сервису нужен только список партнёрских хостов, allowlist надёжнее любых диапазонов.

### Проверили хост, забыли редиректы и схему

```go
// УЯЗВИМО: CheckRedirect не задан — до 10 редиректов куда угодно.
c := &http.Client{Transport: tr}

// Правильно: лимит и схемы; адрес каждого хопа проверит ControlContext.
c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
	if len(via) > 3 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		return errors.New("редирект запрещён")
	}
	return nil
}
```

Защита в `CheckRedirect` по URL нового хопа — строковая, со всеми проблемами первой ловушки. Его дело — лимит и схемы, а адреса пусть проверяет `ControlContext`.

## Задание

Напишите клиента для запросов по URL от пользователей:

```go
var ErrBlocked error // соединение с адресом запрещено

func IsPublic(a netip.Addr) bool
func NewClient(allow func(netip.AddrPort) bool) *http.Client
```

- **`IsPublic`** возвращает `true`, только если адрес после `Unmap()` — глобальный unicast (`IsGlobalUnicast`), не частный (`IsPrivate`) и не входит в `100.64.0.0/10` и `0.0.0.0/8`. Для нулевого `netip.Addr{}` — `false`.
- **`NewClient`** возвращает клиента, который открывает TCP-соединение только с адресом, для которого `allow` вернул `true`. Это касается первого запроса, каждого редиректа и любых имён после DNS. `allow` получает IP и порт, к которым идёт подключение; IPv4-mapped IPv6 — в виде IPv4. Если `allow == nil`, разрешены адреса, для которых `IsPublic(addr)` истинно, с любым портом. Запрос к запрещённому адресу завершается ошибкой, для которой `errors.Is(err, ErrBlocked)`, а сервер по этому адресу не получает ни одного соединения.
- Схемы — только `http` и `https`: запрос по другой схеме, в том числе редирект на неё, — ошибка.
- Редиректы клиент проходит, но не больше трёх подряд: цепочка из трёх редиректов завершается ответом, а на четвёртом редиректе `Get` возвращает ошибку.
- `Timeout` клиента — больше нуля и не больше 30 секунд.
- Клиент используется конкурентно (тесты идут с `-race`). В тестах «внешние» и «внутренние» серверы живут на `127.0.0.1` и различаются портом — поэтому политика получает и порт.
