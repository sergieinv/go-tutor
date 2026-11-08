---
title: Ошибки, context и остановка сервиса
summary: Доменные ошибки и их обёртывание по слоям, перевод в HTTP-статус в одном месте на краю, дедлайн запроса до SQL через QueryContext, отмена клиентом, graceful shutdown сервиса с БД и фоновыми воркерами в правильном порядке.
difficulty: 4
minutes: 40
prerequisites:
  - 04-errors/03-custom-errors
  - 04-errors/04-wrapping
  - 05-concurrency/19-errgroup
  - 08-web/07-request-context
  - 08-web/08-graceful-shutdown
  - 08-web/10-database-sql
docs:
  - title: "Go Blog: Working with Errors in Go 1.13"
    url: https://go.dev/blog/go1.13-errors
  - title: "Go Blog: Contexts and structs"
    url: https://go.dev/blog/context-and-structs
  - title: "Go: Canceling in-progress database operations"
    url: https://go.dev/doc/database/cancel-operations
runner:
  race: true
---

Как устроены ошибки, `context` и `Shutdown` по отдельности, разобрано в модулях 4, 5 и 8. В сервисе важно, как они проходят **сквозь слои** — хранилище, сервис, HTTP — и в каком порядке всё останавливается.

**Ошибки по слоям.** Сервис объявляет доменные ошибки: sentinel-значения для исходов, на которые реагируют (`ErrNotFound`, `ErrConflict`), и тип с полями, когда нужны подробности (`*ValidationError{Field, Msg}`). Каждый слой делает своё:

- **хранилище** переводит ошибки драйвера в доменные там, где это исход, а не сбой: `sql.ErrNoRows` → `ErrNotFound`, нарушение уникальности → `ErrConflict` (коды PostgreSQL и MySQL — уроки 11 и 14). Остальное возвращает как есть — это сбой;
- **сервис** проверяет ввод и оборачивает всё, что пришло снизу, контекстом операции: `fmt.Errorf("reserve item %d: %w", id, err)`. Хендлер не должен знать о `sql.ErrNoRows` — тогда хранилище можно заменить, не трогая HTTP;
- **край** (HTTP-хендлер, воркер очереди, `main`) решает, что делать. В HTTP это одна функция `writeError`, которая проходит цепочку через `errors.Is`/`errors.As` и выбирает статус: проверка ввода — `400`, не найден — `404`, конфликт — `409`, таймаут — `503`, всё остальное — `500`. Только здесь пишется лог, и только для неожиданных ошибок: `404` на несуществующий id — нормальный ответ, а не повод будить дежурного. Клиент получает общий текст, подробности (`no such table`, адрес базы, SQL) — только в лог.

**Контекст до SQL.** Хендлер передаёт `r.Context()` в сервис, сервис — в хранилище, хранилище — в `QueryContext`/`ExecContext`. Тогда работает и отмена клиентом (соединение закрыто — контекст отменён), и дедлайн: `context.WithTimeout(r.Context(), …)` на долгую операцию. Драйвер, получив отмену, бросает запрос: `modernc.org/sqlite` прерывает его (`sqlite3_interrupt`), а pgx и `go-sql-driver/mysql` по умолчанию рвут соединение — горутина освобождается сразу, но сервер может ещё какое-то время дорабатывать запрос. Поэтому серверный лимит (`statement_timeout` в PostgreSQL, `max_execution_time` для `SELECT` в MySQL) полезен и при правильно прокинутом контексте. Ошибка при этом — `context.DeadlineExceeded` или `context.Canceled`, обёрнутая или нет, поэтому край проверяет её через `errors.Is`. Работу, которая должна пережить запрос (аудит после ответа), отвязывают от его отмены через `context.WithoutCancel(ctx)` — значения остаются, отмены нет — и дают ей собственный таймаут.

**Остановка.** `signal.NotifyContext` превращает `SIGTERM` в отмену контекста, а дальше порядок такой, чтобы каждый шаг закрывал то, чем пользовались предыдущие:

1. `srv.Shutdown(ctx)` — перестать принимать соединения и дождаться активных запросов;
2. отменить контекст фоновых воркеров (отправка outbox, чистка резервов) и дождаться их — `errgroup.Wait` или `WaitGroup.Wait`. У воркеров **свой** контекст, а не контекст сигнала: иначе они остановятся одновременно с HTTP, и обработчик может положить в очередь событие, которое некому отправить;
3. `db.Close()` — пул нужен до последнего запроса и последнего воркера. Поэтому `defer db.Close()` в `run` стоит раньше запуска сервера: отложенные вызовы выполняются в обратном порядке.

Пример собирает это вместе: шесть запросов, у каждого свой путь ошибки (среди них — отчёт, который прерывает дедлайн в 50 мс, и сломанный SQL, детали которого остаются в логе), затем остановка по сигналу.

## Как это в C#

- **Исключения и middleware** → значения ошибок и `writeError`. В ASP.NET Core перевод исключений в статусы делают `IExceptionHandler` (.NET 8+) или `UseExceptionHandler` + `ProblemDetails`; в Go это обычная функция, которую вызывает каждый хендлер. Иерархии исключений нет — роль `catch (NotFoundException)` играет `errors.Is(err, ErrNotFound)`, роль фильтра по типу с полями — `errors.As`.
- **`CancellationToken`** → `ctx`. `HttpContext.RequestAborted` ≈ `r.Context()`, `CancellationTokenSource.CreateLinkedTokenSource` + `CancelAfter` ≈ `context.WithTimeout(r.Context(), d)`. Отмена в .NET — исключение `OperationCanceledException`; в Go — ошибка `context.Canceled`/`DeadlineExceeded`, которую видно в цепочке `%w`.
- **Generic Host** останавливает Kestrel, потом `IHostedService.StopAsync` и `BackgroundService` с `stoppingToken`, не дольше `HostOptions.ShutdownTimeout` (30 с). В Go этот порядок — три вызова в `run`, и их видно: сервер, воркеры, база.

## Ловушки

### Хендлер знает о `sql.ErrNoRows`

```go
order, err := svc.Order(r.Context(), id)
if errors.Is(err, sql.ErrNoRows) { // работает, пока хранилище — database/sql
	http.Error(w, "not found", http.StatusNotFound)
	return
}
```

С pgx напрямую ошибка будет `pgx.ErrNoRows`, с фейком в тестах — своя, и `404` молча превратится в `500`. Переводите ошибки драйвера в доменные в хранилище, а край пусть знает только о `ErrNotFound`.

### `err.Error()` клиенту

```go
http.Error(w, err.Error(), http.StatusInternalServerError)
// клиент видит: reserve item 1: SQL logic error: no such table: items (1)
```

Обёртки по слоям делают текст ошибки подробным — это хорошо для лога и плохо для ответа: в нём имена таблиц, адреса, иногда данные. Для `500` наружу — общий текст (и, если есть, id запроса для поддержки), полный текст — в лог. Подробнее — в [уроке о секретах, логах и ошибках](/09-security/11-secrets-logs-errors/).

### Новый контекст посреди запроса

```go
func (s *Service) Reserve(ctx context.Context, id int64, qty int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // ctx запроса потерян
	defer cancel()
	return s.store.Reserve(ctx, id, qty)
}
```

Таймаут есть, но клиент давно ушёл, а запрос в базе продолжает работать до своих пяти секунд — и держит соединение пула. Дедлайн всегда наследуется от входящего контекста: `context.WithTimeout(ctx, …)`. Тогда срабатывает то, что наступит раньше.

## Задание

Напишите сервис и HTTP-обработчик склада поверх хранилища `Store` (его реализацию дают тесты — в том числе такую, которая ждёт отмены контекста, как зависший запрос к базе).

```go
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type ValidationError struct{ Field, Msg string }
func (e *ValidationError) Error() string // дано: Field + ": " + Msg

type Item struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Stock int    `json:"stock"`
}

type Store interface {
	Item(ctx context.Context, id int64) (Item, error)
	Reserve(ctx context.Context, id int64, qty int) error
}

func NewService(store Store) *Service // дано
func (s *Service) Item(ctx context.Context, id int64) (Item, error)
func (s *Service) Reserve(ctx context.Context, id int64, qty int) error
func NewHandler(svc *Service, log *slog.Logger) http.Handler
```

Сервис:

- `Item`: при `id <= 0` — `Item{}` и `&ValidationError{Field: "id", Msg: "must be positive"}`, хранилище не вызывается. Иначе вызывает `store.Item` с тем же `ctx`; ошибку хранилища возвращает обёрнутой через `%w` (так, что `errors.Is`/`errors.As` находят исходную ошибку) с `id` в тексте, результат при ошибке — `Item{}`.
- `Reserve`: при `id <= 0` — ошибка с `Field: "id"`, иначе при `qty <= 0` — `&ValidationError{Field: "qty", Msg: "must be positive"}`; хранилище в обоих случаях не вызывается. Иначе вызывает `store.Reserve` с тем же `ctx`; ошибку оборачивает так же.

Обработчик (`NewHandler`) передаёт сервису контекст запроса и отвечает JSON с `Content-Type: application/json`:

- `GET /items/{id}` — `200` и JSON товара;
- `POST /items/{id}/reserve` с телом `{"qty": N}` — `204` без тела. Тело, которое не разбирается как JSON (лимит — 1 МиБ), — `400` и `{"error": "invalid json"}`;
- `{id}`, который не разбирается как `int64`, — `400` и `{"error": "id: must be integer"}`.

Ошибки сервиса переводятся в ответ в одном месте, по цепочке `errors.Is`/`errors.As`:

- `*ValidationError` — `400` и `{"error": "<ve.Error()>"}`;
- `ErrNotFound` — `404` и `{"error": "not found"}`;
- `ErrConflict` — `409` и `{"error": "conflict"}`;
- `context.DeadlineExceeded` или `context.Canceled` — `503` и `{"error": "timeout"}`;
- любая другая — `500` и `{"error": "internal error"}`.

Для `500` в `log` пишется ровно одна запись уровня `Error` с атрибутом `err` — самой ошибкой (её полный текст); клиенту подробности не показываются. В остальных случаях записей уровня `Error` нет.
