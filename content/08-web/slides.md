# Сеть и веб

Обзор модуля для показа команде: HTTP-клиент и сервер, маршруты, middleware, JSON API, остановка, TCP и `database/sql` — на `net/http` без фреймворка и конвейера ASP.NET.

---

## HTTP-клиент: таймаут — ваша забота

```go
var client = &http.Client{Timeout: 10 * time.Second} // один на программу

req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
resp, err := client.Do(req) // err — только если ответа нет
if err != nil {
	return err
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusOK { /* 404 и 500 — не ошибка Do */ }
```

- У `http.Get` и `DefaultClient` таймаута нет вовсе (в .NET — 100 с).
- `EnsureSuccessStatusCode()` нет; тело читают через `io.LimitReader`.

---

## Сервер — один интерфейс

```go
type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}
```

| C# | Go |
|---|---|
| Kestrel + `WebApplication` | `http.Server` + `http.ServeMux` |
| хост, DI, конвейер | `srv.Serve(ln)` — обычный вызов в `main` |
| лимиты Kestrel по умолчанию | все таймауты `http.Server` нулевые — задайте `ReadHeaderTimeout` |

Каждое соединение — своя горутина: общее состояние хендлеров — под мьютексом.

---

## `ResponseWriter`: строгий порядок

```go
w.Header().Set("Location", "/items/1") // 1. заголовки
w.WriteHeader(http.StatusCreated)      // 2. статус
w.Write(body)                          // 3. тело
```

- Заголовок после `WriteHeader` молча теряется — исключения, как в ASP.NET, не будет.
- `http.Error` только пишет ответ, хендлер продолжает работу. После него — всегда `return`.

---

## Маршруты на `ServeMux` (Go 1.22+)

```go
mux.HandleFunc("GET /books/{id}", getBook) // r.PathValue("id") — строка
mux.HandleFunc("GET /files/{path...}", serveFile)
mux.HandleFunc("GET /{$}", home)           // только "/", а "GET /" — всё подряд
```

- Порядок регистрации не важен: побеждает более специфичный шаблон.
- Конфликт шаблонов — паника при регистрации, то есть при старте.
- Нет `{id:int}`: разбор и проверку параметра пишет хендлер. 405 с `Allow` — из коробки.

---

## Middleware — функция над `Handler`

```go
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Println(r.Method, r.URL.Path, time.Since(start))
	})
}

handler := logging(recoverer(requestID(mux))) // снаружи — первым и последним
```

`app.Use(...)` → вложенность выражения. Паника хендлера без своего `recoverer` даёт обрыв соединения, а не 500.

---

## Путь запроса изнутри

Как запрос проходит `http.Server`, обёртки и `ServeMux`, как возвращается ответ и как обрыв клиента отменяет `r.Context()`.

@viz 08-web/04-middleware

---

## JSON API: строгий разбор вручную

- Нет model binding и `[ApiController]` — три хелпера на проект: `decodeJSON`, `writeJSON`, `writeProblem`.
- `http.MaxBytesReader` (413), проверка `Content-Type` (415), `DisallowUnknownFields()` (400).
- Валидация — метод `Validate() error` у DTO; ошибки полей — 422, конфликт — 409.
- Ошибки — `application/problem+json` по RFC 9457, как `ProblemDetails`.
- Лимита тела по умолчанию нет: без `MaxBytesReader` клиент пришлёт гигабайт.

---

## Контекст запроса и мягкая остановка

```go
go func() {
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}()
<-ctx.Done() // SIGTERM через signal.NotifyContext
shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // не от ctx!
defer cancel()
srv.Shutdown(shCtx) // закрывает слушатель и ждёт активные запросы
```

- `r.Context()` — это `RequestAborted`: передавайте его в базу и исходящие запросы.
- Фоновой работе после ответа — `context.WithoutCancel(r.Context())`.

---

## TCP и `database/sql`

- TCP — поток байт, а не сообщений: «ровно N байт» читает `io.ReadFull`, длину из сети сверяют с лимитом.
- Горутина на соединение и блокирующий `Read` — норма; рантайм ждёт через epoll/kqueue.
- `*sql.DB` — пул, а не соединение: создают один раз, связь проверяют `PingContext`.

| C# | Go |
|---|---|
| `DbConnection` + `DbCommand` | `db.QueryContext(ctx, "… WHERE id = ?", id)` |
| `using var reader` | `defer rows.Close()` + `rows.Err()` после цикла |
| `DBNull.Value` | `sql.NullString`, `sql.Null[T]` или указатель |

---

## Что запомнить

- Клиенту и серверу таймауты задают явно — по умолчанию их нет.
- Статус 4xx/5xx — не ошибка `client.Do`; тело закрывают и дочитывают.
- После `http.Error` — `return`; заголовки — до `WriteHeader`.
- `r.Context()` — во все вызовы; `Shutdown` с лимитом от `context.Background()`.
- `defer rows.Close()` и `defer tx.Rollback()` сразу после успешного вызова.
