---
title: XSS и контекстное экранирование
summary: html/template против text/template, контексты HTML, атрибута, URL и JS — одно значение экранируется по-разному, template.HTML/URL/JS как дыры, javascript:-ссылки и #ZgotmplZ, JSON в <script>, Content-Type, nosniff и CSP.
difficulty: 4
minutes: 35
prerequisites:
  - 06-stdlib/03-text-templates
  - 08-web/02-http-server
  - 09-security/02-input-validation
runner:
  race: true
docs:
  - title: "Пакет html/template"
    url: https://pkg.go.dev/html/template
  - title: "OWASP: Cross Site Scripting Prevention Cheat Sheet"
    url: https://cheatsheetseries.owasp.org/cheatsheets/Cross_Site_Scripting_Prevention_Cheat_Sheet.html
  - title: "MDN: Content-Security-Policy"
    url: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy
---

XSS (cross-site scripting) — данные пользователя, которые браузер принял за код: имя `<script>…</script>` в профиле, ссылка `javascript:…`, строка, закрывшая `</script>` раньше времени. Скрипт выполняется в origin вашего сайта — с куками сессии, доступом к API и формам. Бывает отражённый (нагрузка в параметре запроса), хранимый (в базе, видят все) и DOM-XSS (собирает сам JavaScript страницы — серверное экранирование от него не спасает).

Главная защита в Go — `html/template`. API у него тот же, что у `text/template`, но при первом выполнении он разбирает **разметку** шаблона, отслеживает контекст каждого действия `{{…}}` — текст, значение атрибута, URL, JavaScript, CSS — и дописывает в действие свой экранировщик. Поэтому одно и то же `Tom & "Jerry"` в тексте станет `Tom &amp; &#34;Jerry&#34;`, в параметре ссылки — `Tom%20%26%20%22Jerry%22`, в `<script>` — JSON-строкой `"Tom \u0026 \"Jerry\""`. В начале `href`/`src` действует ещё и фильтр: схемы, кроме `http`, `https` и `mailto`, заменяются безвредным `#ZgotmplZ`. Значение в CSS-контексте, похожее на что-то кроме простых токенов, тоже превращается в `ZgotmplZ`. `text/template` не делает ничего из этого — для HTML он не годится.

Экранирование выключают типы-«метки доверия»: `template.HTML`, `template.HTMLAttr`, `template.URL`, `template.JS`, `template.JSStr`, `template.CSS`, `template.Srcset`. Значение такого типа вставляется как есть — это обещание «я проверил». Каждое приведение строки пользователя к ним — потенциальная дыра; безопасно приводить только константы и то, что вы собрали сами из уже экранированных частей.

Шаблон — первый рубеж, заголовки — второй. Без `Content-Type` `net/http` угадывает тип по первым 512 байтам (`http.DetectContentType`), и загруженный «аватар» с HTML уйдёт как `text/html`. `X-Content-Type-Options: nosniff` запрещает браузеру угадывать тип сам. `Content-Security-Policy` ограничивает, откуда браузер берёт скрипты: с `script-src 'self'` он не выполнит встроенные `<script>` и обработчики `onclick=…`, даже если нагрузка в страницу попала (свои встроенные скрипты тогда помечают `nonce` из `crypto/rand` на каждый ответ или выносят в файлы).

## Как это в C#

- Razor экранирует `@Model.Name` всегда, `@Html.Raw(x)` выключает экранирование — это `template.HTML`. Но Razor знает только HTML-контекст: `href="@url"` с `javascript:alert(1)` он закодирует как HTML и **пропустит**, а `html/template` заменит на `#ZgotmplZ`.
- Данные в `<script>` в Razor пишут через `@Json.Serialize(model)` — кодировщик по умолчанию экранирует `<`, `>`, `&`. В `html/template` это происходит само для любого значения в JS-контексте: передайте структуру, а не готовую строку JSON.
- Без Razor (минимальные API, `Results.Content`) кодировщики `HtmlEncoder`, `UrlEncoder`, `JavaScriptEncoder` выбираете вручную под каждый контекст — так же, как пришлось бы в Go без `html/template`.
- Заголовки безопасности ASP.NET Core по умолчанию не ставит ни `nosniff`, ни CSP — их добавляют middleware. В Go — одна обёртка над `http.Handler`, которая выставляет их до вызова `next`.

## Под капотом

Визуализатор проходит по шаблону так же, как экранировщик `html/template` при первом `Execute`: состояние меняется на каждом теге и атрибуте, а в действия дописываются функции `_html_template_*`. Значение `template.HTML` проходит через тот же экранировщик, но тот видит тип и пропускает строку без изменений.

## Ловушки

### `text/template` вместо `html/template`

```go
import "text/template" // УЯЗВИМО: импорт подставила IDE, API тот же

t := template.Must(template.New("p").Parse(`<p>{{.}}</p>`))
t.Execute(w, `<script>alert(1)</script>`) // <p><script>alert(1)</script></p>
```

Пакеты взаимозаменяемы по API, и `goimports` может выбрать не тот. Для HTML — только `html/template`; в ревью ищите `text/template` рядом с `http.ResponseWriter`.

### `template.HTML` из строки пользователя

```go
// УЯЗВИМО: «чтобы работали переносы строк» — и заодно <img onerror=…>
bio := template.HTML(strings.ReplaceAll(u.Bio, "\n", "<br>"))

// Правильно: экранировать всё, потом добавить свою разметку.
bio := template.HTML(strings.ReplaceAll(template.HTMLEscapeString(u.Bio), "\n", "<br>"))
```

То же с `template.URL(u.Website)` — она отключает фильтр схем, и `javascript:` проходит в `href` — и с `template.JS`. Если нужна богатая разметка от пользователя, это задача HTML-санитайзера со списком разрешённых тегов (в стандартной библиотеке его нет), а не приведения типа.

### JSON в `<script>` без экранирования

```go
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false) // УЯЗВИМО: "</script><script>…" закроет тег
enc.Encode(user)
data := template.JS(buf.String())
```

Браузер ищет `</script>` в тексте скрипта, не разбирая JavaScript, поэтому строка с этим текстом закрывает тег посреди литерала. `json.Marshal` по умолчанию пишет `<`, `>`, `&` как `\u003c`, `\u003e`, `\u0026`, а ещё проще — передать в `{{.}}` внутри `<script>` саму структуру: `html/template` закодирует её в JSON сам.

## Задание

Код в `task.go` работает, но уязвим к XSS. Найдите и закройте уязвимости, не меняя разметку страницы (`pageTmpl`) и поведение для обычных данных.

```go
type Profile struct {
	Name    string   `json:"name"`
	Bio     string   `json:"-"`
	Website string   `json:"-"`
	Tags    []string `json:"tags"`
}

func FormatBio(s string) template.HTML
func RenderProfile(w io.Writer, p Profile) error
func ProfileHandler(p Profile) http.Handler
func AvatarHandler(data []byte) http.Handler
```

Все поля `Profile` и байты аватара присылает пользователь. Ни одно из них не должно добавить в страницу разметку, скрипт или ссылку со схемой вроде `javascript:`, а загруженный файл не должен открыться в браузере как страница.

- `FormatBio` — разметка биографии: `**текст**` становится `<b>текст</b>` (пары `**` ищутся слева направо, самые короткие; непарные `**` остаются как есть), перевод строки — `<br>`. Остальной текст пользователь видит ровно таким, каким ввёл.
- `RenderProfile` выполняет `pageTmpl`: имя в `<title>` и `<h1>`, биография из `FormatBio` в `<div class="bio">`, ссылка на `Website`, если он не пустой (обычные `http`/`https`-адреса и относительные пути работают как ссылки), и в `<script>` — значение `profile`: объект JSON `{"name": …, "tags": …}` (для `nil`-тегов — `null`), который разбирается обратно в те же данные.
- `ProfileHandler` отвечает `200` со страницей, `Content-Type: text/html; charset=utf-8` и `X-Content-Type-Options: nosniff`; при ошибке рендера — `500`.
- `AvatarHandler` отдаёт байты как есть; PNG, JPEG и GIF — с типом `image/png`, `image/jpeg`, `image/gif`.
