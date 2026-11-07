---
title: REST и JSON
summary: JSON API на net/http — строгое чтение тела (Content-Type, MaxBytesReader, DisallowUnknownFields, лишние данные), коды 200/201/204/400/404/409/413/415/422, Location, ошибки в формате application/problem+json и хелпер writeJSON.
difficulty: 4
minutes: 40
prerequisites:
  - 08-web/03-routing
  - 06-stdlib/05-json
docs:
  - title: "Пакет encoding/json: Decoder"
    url: https://pkg.go.dev/encoding/json#Decoder
  - title: "RFC 9457: Problem Details for HTTP APIs"
    url: https://www.rfc-editor.org/rfc/rfc9457
  - title: "Пакет net/http: MaxBytesReader"
    url: https://pkg.go.dev/net/http#MaxBytesReader
runner:
  race: true
---

В Go нет model binding и `[ApiController]`: хендлер сам читает тело, сам решает, что считать ошибкой, и сам пишет ответ. Звучит как много ручной работы, но на практике это три хелпера, которые пишут один раз на проект: `decodeJSON` (прочитать и проверить тело), `writeJSON` (статус + `Content-Type` + `json.Encoder`) и `writeProblem` (ответ-ошибка в едином формате).

Строгое чтение тела — несколько шагов, каждый со своим кодом ответа. Проверить `Content-Type` запроса через `mime.ParseMediaType` (иначе `415 Unsupported Media Type`). Ограничить размер: `http.MaxBytesReader(w, r.Body, n)` вернёт ошибку `*http.MaxBytesError` после `n` байт и заодно попросит сервер закрыть соединение (`413`, в RFC 9110 — Content Too Large, в Go — `http.StatusRequestEntityTooLarge`). Декодировать через `json.NewDecoder` с `DisallowUnknownFields()`, чтобы опечатка в имени поля не прошла молча (`400`). И убедиться, что после объекта ничего нет: `Decoder.Decode` читает одно значение и на хвост `{"a":1}{"b":2}` не смотрит. Синтаксически верный JSON с неверными данными — пустое имя, плохой email — это уже `422` (`http.StatusUnprocessableEntity`), а конфликт с состоянием (такой email уже есть) — `409 Conflict`.

Для ошибок удобен стандартный формат RFC 9457: `Content-Type: application/problem+json` и объект с полями `type`, `title`, `status`, `detail`. Клиент разбирает все ошибки одинаково, а `type: "about:blank"` означает «смысл ошибки — в статусе». Успешное создание — `201 Created` с заголовком `Location` на новый ресурс, удаление — `204 No Content` без тела.

## Как это в C#

- `[ApiController]` с model binding сам разбирает тело, проверяет атрибуты `[Required]`, `[EmailAddress]` и возвращает `400` с `ValidationProblemDetails`. В Go это ваш код: `decodeJSON` + проверки + `writeProblem`. Зато видно каждый шаг, и коды (`400` против `422`) вы выбираете сами.
- `Results.Problem(...)`/`ProblemDetails` ≈ структура `problem` и `writeProblem`. Формат тот же — RFC 9457 (бывший RFC 7807).
- `[Consumes("application/json")]` даёт `415`; `[RequestSizeLimit]` и лимит Kestrel (30 МБ по умолчанию) — `413`. В Go лимита тела по умолчанию нет вовсе — без `MaxBytesReader` клиент может прислать гигабайт.
- `System.Text.Json` по умолчанию игнорирует лишние поля, как и `encoding/json`; строгий режим — `JsonUnmappedMemberHandling.Disallow` против `DisallowUnknownFields()`. `Results.Created(uri, value)` ≈ `w.Header().Set("Location", ...)` + `writeJSON(w, 201, v)`.

## Ловушки

### `map[string]any` теряет типы

```go
var m map[string]any
json.NewDecoder(r.Body).Decode(&m)
qty := m["qty"].(int) // паника: числа в any — float64
```

Разбор в `map[string]any` не проверяет ни поля, ни типы, а все числа превращает в `float64` (и большие id теряют точность после 2⁵³). Декодируйте в структуру запроса: `DisallowUnknownFields` отсечёт лишнее, а неверный тип даст понятный `*json.UnmarshalTypeError`.

### `omitempty` прячет значимый ноль

```go
type item struct {
	Qty  int  `json:"qty,omitempty"`  // qty = 0 пропадёт из ответа
	Done bool `json:"done,omitempty"` // done = false — тоже
}
```

Для клиента «поля нет» и «поле равно 0» — разные вещи. В ответах API `omitempty` ставьте только там, где отсутствие — осознанная часть контракта. А в запросах отличить «не прислали» от «прислали 0» поможет указатель: `Qty *int` будет `nil`, если поля не было.

### Забытый `return` после ошибки

```go
if err := decode(r, &in); err != nil {
	writeProblem(w, http.StatusBadRequest, err.Error())
}
writeJSON(w, http.StatusCreated, save(in)) // сохранит мусор и допишет второй JSON
```

`writeProblem`, как и `http.Error`, только пишет ответ. Без `return` хендлер продолжит работу: создаст ресурс из неполных данных, вызовет лишний `WriteHeader` (в журнале — `superfluous response.WriteHeader call`) и допишет к ответу второй JSON-объект.

## Задание

Напишите API пользователей:

```go
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func NewAPI() http.Handler
```

Каждый вызов `NewAPI` создаёт пустое хранилище. Хендлеры вызываются конкурентно (тесты идут с `-race`).

**Ответы.** Успешный ответ с телом — `Content-Type: application/json` и пользователь как JSON-объект ровно с полями `id`, `name`, `email`. Ошибка — `Content-Type: application/problem+json` и `Problem` с `type` = `"about:blank"`, `title` = `http.StatusText(status)`, `status` = код ответа и непустым `detail` (текст на ваш выбор).

**`POST /users`** — создать пользователя. Проверки по порядку, первая неудача определяет ответ:

1. медиатип `Content-Type` запроса не `application/json` (параметры вроде `charset` допустимы; заголовка нет — тоже ошибка) — `415`;
2. тело длиннее 4096 байт — `413`;
3. тело — не ровно один JSON-объект с полями только `name` и `email` (строки): синтаксическая ошибка, пустое тело, `null` или не объект, неизвестное поле (в том числе `id`), значение не того типа, данные после объекта — `400`. Поля сравниваются как в `encoding/json` (без учёта регистра), а `null` в значении поля — не ошибка типа: поле остаётся пустой строкой и дальше проверяется как пустое;
4. `name` после `strings.TrimSpace` пустой или длиннее 100 символов (рун), либо `email` не вида `локальная@домен` — ровно один `@`, обе части непустые, без пробельных символов — `422`;
5. пользователь с таким `email` уже есть (сравнение без учёта регистра, `strings.EqualFold`) — `409`.

При успехе — `201`, заголовок `Location: /users/<id>` и созданный пользователь в теле. `id` выдаются с 1 по порядку и не переиспользуются, `name` сохраняется после `TrimSpace`, `email` — как прислан.

**`GET /users/{id}`** — `200` и пользователь; **`DELETE /users/{id}`** — `204` без тела. Если `{id}` — не положительное десятичное целое без знака и ведущих нулей или такого пользователя нет — `404` в формате `Problem`. Удалённый email можно зарегистрировать снова.
