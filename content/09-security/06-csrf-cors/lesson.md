---
title: CSRF и CORS
summary: Почему cookie-сессии уязвимы к CSRF, http.CrossOriginProtection из Go 1.25 (Sec-Fetch-Site, Origin, доверенные origin, обход для путей), SameSite, Secure и HttpOnly, CORS как ослабление same-origin policy и его типичные ошибки, заголовки безопасности.
difficulty: 4
minutes: 35
prerequisites:
  - 08-web/04-middleware
  - 08-web/02-http-server
  - 09-security/05-xss-escaping
runner:
  race: true
docs:
  - title: "Пакет net/http: CrossOriginProtection"
    url: https://pkg.go.dev/net/http#CrossOriginProtection
  - title: "MDN: Cross-Origin Resource Sharing (CORS)"
    url: https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/CORS
  - title: "MDN: Sec-Fetch-Site"
    url: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Sec-Fetch-Site
---

**CSRF** (cross-site request forgery): пользователь вошёл в банк, потом открыл `evil.test`, а там форма `<form action="https://bank.example/transfer" method="post">` с автоотправкой. Браузер отправит её и приложит куку сессии банка — для сервера это обычный запрос пользователя. Уязвимо всё, что аутентифицируется **автоматически**: куки, HTTP Basic, клиентские сертификаты. API с токеном в заголовке `Authorization` CSRF не боится — чужая страница не может его подставить.

Первая линия — атрибуты куки. `SameSite=Lax` (а в Chrome это поведение по умолчанию для кук без атрибута) не прикладывает куку к кросс-сайтовым `POST`, `Strict` — вообще ни к каким кросс-сайтовым запросам, включая переход по ссылке. Но «сайт» для `SameSite` — это регистрируемый домен: `blog.bank.example` и `bank.example` — один сайт, и взломанный поддомен куку получит. Остальные атрибуты сессионной куки: `HttpOnly` (не видна JavaScript — XSS не украдёт её напрямую), `Secure` (только HTTPS), `Path=/`, без `Domain`, если поддоменам она не нужна.

Вторая линия — проверка, откуда пришёл запрос. В ASP.NET это антифорджери-токены; в Go 1.25 появился `http.CrossOriginProtection` без токенов и состояния. Он пропускает безопасные методы (`GET`, `HEAD`, `OPTIONS`) и смотрит на `Sec-Fetch-Site`, который все современные браузеры ставят сами и который страница подделать не может: `same-origin` и `none` (пользователь сам набрал адрес) — пропустить, `cross-site` и `same-site` — отклонить с `403`. Нет `Sec-Fetch-Site` — сравнивается хост из `Origin` с `Host`; нет и `Origin` — это не браузер (curl, сервис), запрос проходит. Исключения: `AddTrustedOrigin("https://app.example")` — доверенный фронтенд на другом origin, `AddInsecureBypassPattern("POST /webhooks/")` — пути, которые аутентифицируются не кукой (подпись вебхука), `SetDenyHandler` — свой ответ на отказ.

**CORS** — не защита, а **ослабление** same-origin policy. Без него скрипт с `app.example` может *отправить* запрос на `api.example`, но не *прочитать* ответ. Заголовок `Access-Control-Allow-Origin: https://app.example` разрешает чтение этому origin, `Access-Control-Allow-Credentials: true` — с куками. Для «непростых» запросов (методы кроме `GET`/`HEAD`/`POST`, `Content-Type: application/json`, свои заголовки) браузер сначала шлёт preflight — `OPTIONS` с `Access-Control-Request-Method`, — и без разрешения сам запрос не отправит. Но HTML-форма — простой запрос: preflight нет, CORS её не остановит. Поэтому CORS и CSRF-защита нужны обе и решают разные задачи.

## Как это в C#

- Антифорджери-токены ASP.NET (`AddAntiforgery`, `[ValidateAntiForgeryToken]`, `UseAntiforgery` для минимальных API) → `http.CrossOriginProtection`: не нужны скрытые поля в формах и кука с токеном, проверка — по заголовкам браузера. Он же в Go — единственное, что есть «из коробки»: middleware нужно подключить самому.
- `CookieOptions { HttpOnly, Secure, SameSite }` → поля `http.Cookie` с теми же именами. `SameSite` у Go — `http.SameSiteLaxMode`, `StrictMode`, `NoneMode`; если не задать, атрибута в `Set-Cookie` не будет.
- `AddCors` + `WithOrigins(...)` → свой middleware на десяток строк. ASP.NET Core бросает исключение на `AllowAnyOrigin()` вместе с `AllowCredentials()`; в Go эту ошибку никто не поймает, а «отразить `Origin`», чтобы обойти запрет браузера на `*` с куками, — частая уязвимость.
- `UseHsts`, `X-Frame-Options` (антифорджери ASP.NET ставит `SAMEORIGIN` сам) → заголовки в своей обёртке: `Strict-Transport-Security`, `Content-Security-Policy: frame-ancestors 'none'`, `Referrer-Policy`, `X-Content-Type-Options`.

## Ловушки

### `GET`, который меняет состояние

```go
// УЯЗВИМО: <img src="https://bank.example/logout"> или ссылка на
// /orders/42/delete выполнит действие от имени жертвы.
mux.HandleFunc("GET /orders/{id}/delete", deleteOrder)
```

`CrossOriginProtection` не проверяет безопасные методы, а `SameSite=Lax` прикладывает куку к переходу по ссылке. Всё, что меняет данные, — только `POST`/`PUT`/`PATCH`/`DELETE`; шаблоны `ServeMux` с методом (`"POST /orders/{id}/delete"`) делают это явным.

### Отражение `Origin` и проверка суффиксом

```go
// УЯЗВИМО: любой сайт читает ответы с куками жертвы.
w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
w.Header().Set("Access-Control-Allow-Credentials", "true")

// УЯЗВИМО: пропускает https://evilapp.example и https://app.example.evil.test.
if strings.HasSuffix(origin, "app.example") || strings.HasPrefix(origin, "https://app.example") { ... }
```

Сравнивайте origin целиком со списком (`map[string]bool`). Не разрешайте `null`: его шлют sandbox-iframe, `file://` и некоторые редиректы — атакующий получит его легко. И добавляйте `Vary: Origin`, иначе кеш отдаст разрешение для одного origin другому.

### CORS вместо защиты от CSRF

```go
// УЯЗВИМО: «без нашего CORS-заголовка браузер запрос не пустит».
mux.Handle("POST /transfer", cors(transfer))
```

Браузер не пустит **чтение ответа**, а сам `POST` из формы уйдёт и выполнится — preflight для форм не делается. Preflight защищает только запросы, которые форма отправить не может (JSON, `PUT`), и то пока сервер не начал принимать `text/plain` «для совместимости». Изменяющие запросы закрывайте `CrossOriginProtection`.

## Задание

Напишите CORS-middleware и обёртку, которая добавляет к ней защиту от CSRF.

```go
var ErrBadOrigin = errors.New("bad origin")

func NewCORS(origins []string) (func(http.Handler) http.Handler, error)
func Protect(origins []string, next http.Handler) (http.Handler, error)
```

**Допустимый origin** в списке — строка, которая разбирается `url.Parse`, имеет схему `http` или `https`, непустой хост и в точности равна `scheme + "://" + host` (хост может включать порт): без пути и `/` в конце, без запроса, фрагмента и `user@`, только строчные буквы. Если в списке есть недопустимая запись (`""`, `*`, `null`, `https://app.example/`…), `NewCORS` и `Protect` возвращают `nil` и ошибку с `errors.Is(err, ErrBadOrigin)`. Пустой список допустим.

Middleware из `NewCORS` на каждый ответ добавляет `Vary: Origin` (`Header().Add`). Origin запроса **разрешён**, если заголовок `Origin` в точности совпадает с одной из записей списка.

- **Preflight** — запрос `OPTIONS` с непустым заголовком `Access-Control-Request-Method`; `next` не вызывается. Если origin разрешён и запрошенный метод — `GET`, `POST`, `PUT` или `DELETE`, ответ `204` с заголовками `Access-Control-Allow-Origin: <origin>`, `Access-Control-Allow-Credentials: true`, `Access-Control-Allow-Methods: GET, POST, PUT, DELETE`, `Access-Control-Allow-Headers: Content-Type`, `Access-Control-Max-Age: 600`. Иначе — `403` без заголовков `Access-Control-*`.
- **Остальные запросы** (включая `OPTIONS` без `Access-Control-Request-Method`) всегда передаются в `next`. Если origin разрешён, перед этим ставятся `Access-Control-Allow-Origin: <origin>` и `Access-Control-Allow-Credentials: true`; иначе заголовков `Access-Control-*` нет.

`Protect(origins, next)` — middleware из `NewCORS` снаружи и защита от CSRF внутри. Запросы с методами `GET`, `HEAD`, `OPTIONS` проходят. Запрос с любым другим методом получает `403` и не доходит до `next`, если:

- заголовок `Sec-Fetch-Site` есть, не равен `same-origin` или `none`, а `Origin` не из списка (в том числе пуст);
- `Sec-Fetch-Site` нет, `Origin` есть, хост из него (с портом) не равен `r.Host`, и `Origin` не из списка.

Остальные запросы проходят. Именно так работает `http.CrossOriginProtection`.
