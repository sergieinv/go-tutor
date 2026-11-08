# Чек-лист ревью Go-кода

Что проверять на ревью Go-сервиса. Каждый пункт — одна проверка и урок курса, где она разобрана подробно. Задачи «приведи к виду для ревью» проверяют пункты этого списка.

Перед чтением кода пусть отработают машины: `gofmt -l .` пуст, `go vet ./...` и `golangci-lint run` чисты, `go test -race ./...` зелёный ([тесты в промышленном проекте](/07-testing/09-testing-practices/)).

## Идиомы и имена

- Имена короткие и без повторов: `user.Service`, а не `user.UserService`; геттер `Owner()`, а не `GetOwner()`; аббревиатуры одним регистром — `userID`, `HTTPClient` — [идиоматичный Go и ревью](/10-services-db/01-idiomatic-review/).
- Receiver — одна-две буквы от типа, одинаковые во всех методах; не `this` и не `self` — [идиоматичный Go и ревью](/10-services-db/01-idiomatic-review/).
- Receiver у всех методов типа одного вида: если хоть один метод меняет состояние или в типе есть мьютекс — указатель — [методы](/03-types/02-methods/).
- Интерфейс маленький и объявлен у потребителя; конструктор возвращает конкретный тип — [идиоматичный Go и ревью](/10-services-db/01-idiomatic-review/), [интерфейсы](/03-types/03-interfaces/).
- Нулевое значение типа полезно (`var b T` работает) или конструктор обязателен и это видно из API — [идиоматичный Go и ревью](/10-services-db/01-idiomatic-review/).
- Пакеты названы по смыслу (`orders`, `stock`), нет `utils`, `common`, `models`; код сервиса — в `internal/` — [раскладка проекта](/10-services-db/02-project-layout/).
- Зависимости собираются явно в `main`/`run`, нет пакетных переменных с состоянием и `init()` с побочными эффектами — [раскладка проекта](/10-services-db/02-project-layout/).
- Конфигурация читается в одном месте, проверяется при старте целиком, секреты не печатаются — [раскладка проекта](/10-services-db/02-project-layout/), [секреты, логи и ошибки](/09-security/11-secrets-logs-errors/).

## Ошибки

- Ни одна ошибка не проигнорирована молча: нет `_ = f()` и `f()` без проверки там, где возвращается `error` (включая `Close` при записи) — [ошибки](/04-errors/01-errors/), [defer](/04-errors/05-defer/).
- Ошибка обёрнута с контекстом через `%w`, а не `%v`, если вызывающему нужна причина — [обёртывание ошибок](/04-errors/04-wrapping/).
- Текст ошибки строчными, без точки в конце, говорит, *что* делали: `load order 7: …` — [идиоматичный Go и ревью](/10-services-db/01-idiomatic-review/).
- Ошибка либо обработана (залогирована, переведена в ответ), либо возвращена — не то и другое сразу — [идиоматичный Go и ревью](/10-services-db/01-idiomatic-review/).
- Ошибки сравниваются через `errors.Is`/`errors.As`, а не `==` и не по тексту — [sentinel-ошибки](/04-errors/02-sentinel-errors/), [свои типы ошибок](/04-errors/03-custom-errors/).
- Ошибки драйвера переводятся в доменные в хранилище; край (хендлер) знает только доменные — [ошибки, context и остановка](/10-services-db/03-errors-context-shutdown/).
- Перевод ошибки в HTTP-статус — в одном месте; клиенту общий текст, подробности — в лог — [ошибки, context и остановка](/10-services-db/03-errors-context-shutdown/), [секреты, логи и ошибки](/09-security/11-secrets-logs-errors/).
- Библиотечный код не паникует на плохом вводе; `panic` — только для невозможного состояния, `recover` — на границе горутины или запроса — [panic и recover](/04-errors/06-panic-recover/).
- Функция, вернувшая ошибку, возвращает нулевые значения остальных результатов (или это явно описано) — [ошибки](/04-errors/01-errors/).

## context и отмена

- `ctx context.Context` — первый параметр каждой функции, которая ходит в сеть, базу или ждёт — [context](/05-concurrency/18-context/).
- Контекст запроса доходит до самого низа: `r.Context()` → сервис → `QueryContext`/`ExecContext`/`NewRequestWithContext` — [контекст запроса](/08-web/07-request-context/), [ошибки, context и остановка](/10-services-db/03-errors-context-shutdown/).
- Нет `context.Background()`/`TODO()` посреди обработки запроса; дедлайн наследуется: `context.WithTimeout(ctx, …)` — [ошибки, context и остановка](/10-services-db/03-errors-context-shutdown/).
- Контекст не хранится в поле структуры — [идиоматичный Go и ревью](/10-services-db/01-idiomatic-review/).
- `cancel` от `WithCancel`/`WithTimeout` вызывается всегда (`defer cancel()`) — [context](/05-concurrency/18-context/).
- Работа, которая должна пережить запрос, отвязана через `context.WithoutCancel` и имеет свой таймаут — [ошибки, context и остановка](/10-services-db/03-errors-context-shutdown/).
- Отмена (`context.Canceled`, `DeadlineExceeded`) отличается от сбоя и не логируется как ошибка — [ошибки, context и остановка](/10-services-db/03-errors-context-shutdown/).

## Ресурсы и горутины

- Ресурс закрывается `defer` сразу после успешного открытия: файлы, `resp.Body`, `rows`, `Stmt`, тикеры — [defer](/04-errors/05-defer/).
- Нет `defer` в цикле по многим ресурсам — тело вынесено в функцию — [defer](/04-errors/05-defer/).
- У каждой горутины есть владелец, способ остановки (`ctx`, закрытый канал) и ожидание (`WaitGroup`, `errgroup`) — [утечки горутин](/05-concurrency/21-goroutine-leaks/), [идиоматичный Go и ревью](/10-services-db/01-idiomatic-review/).
- Горутина не запускается в конструкторе без способа её остановить; `Ticker.Stop` не закрывает канал — [идиоматичный Go и ревью](/10-services-db/01-idiomatic-review/).
- Отправка в канал не может заблокироваться навсегда, если получатель ушёл (буфер, `select` с `ctx.Done()`) — [утечки горутин](/05-concurrency/21-goroutine-leaks/).
- Сервис останавливается по порядку: `Shutdown` HTTP → воркеры → `db.Close()` — [graceful shutdown](/08-web/08-graceful-shutdown/), [ошибки, context и остановка](/10-services-db/03-errors-context-shutdown/).

## Конкурентность

- Общие данные защищены мьютексом или принадлежат одной горутине; `go test -race` чист — [гонки данных](/05-concurrency/22-data-races/), [мьютексы](/05-concurrency/12-mutex/).
- Структура с `sync.Mutex` не копируется (методы на указателе, `go vet` молчит) — [мьютексы](/05-concurrency/12-mutex/).
- Ошибки параллельных задач собираются (`errgroup`), а не теряются в горутинах — [errgroup](/05-concurrency/19-errgroup/).
- Параллелизм ограничен (пул воркеров, семафор, `errgroup.SetLimit`) — [пулы воркеров](/05-concurrency/14-worker-pools/).
- Проверка и действие не разорваны: «прочитал остаток — записал» делается атомарным `UPDATE … WHERE qty >= $1`, блокировкой или версией строки — [блокировки](/10-services-db/07-locking/).
- Повтор при конфликте — ограниченный, с backoff и джиттером, повторяется вся транзакция — [блокировки](/10-services-db/07-locking/), [MVCC и изоляция](/10-services-db/12-postgres-mvcc-isolation/).

## HTTP

- У сервера заданы таймауты (`ReadHeaderTimeout` минимум, обычно и `ReadTimeout`, `WriteTimeout`, `IdleTimeout`) — [HTTP-сервер](/08-web/02-http-server/), [DoS и лимиты](/09-security/10-dos-limits/).
- У клиента есть `Timeout` или контекст с дедлайном; `http.DefaultClient` без таймаута не используется — [HTTP-клиент](/08-web/01-http-client/).
- Тело ответа клиента закрывается и вычитывается; тело запроса ограничено `http.MaxBytesReader` — [HTTP-клиент](/08-web/01-http-client/), [DoS и лимиты](/09-security/10-dos-limits/).
- Ввод проверяется на границе, JSON декодируется строго там, где это важно — [валидация](/08-web/06-validation/), [валидация ввода](/09-security/02-input-validation/).
- `Content-Type` и статус выставлены до записи тела; после ошибки — `return` — [JSON API](/08-web/05-json-api/).
- Права на объект проверяются при его загрузке (`WHERE id = $1 AND owner = $2`), а не только аутентификация — [модель угроз](/09-security/01-threat-model/), [итоговое ревью безопасности](/09-security/13-security-review/).

## Работа с БД

- `*sql.DB`/`pgxpool.Pool` создаётся один раз и передаётся зависимостям; `PingContext` при старте — [database/sql](/08-web/10-database-sql/).
- Пул настроен: `SetMaxOpenConns`, `SetMaxIdleConns`, `SetConnMaxLifetime`/`IdleTime` (или `pgxpool.Config`) согласованы с лимитом соединений базы — [пул соединений](/10-services-db/05-connection-pool/).
- `rows.Close()` через `defer` сразу после `QueryContext`, `rows.Err()` после цикла — [database/sql](/08-web/10-database-sql/), [пул соединений](/10-services-db/05-connection-pool/).
- `QueryRow` всегда заканчивается `Scan`; запрос без результата — `ExecContext` — [database/sql](/08-web/10-database-sql/).
- `sql.ErrNoRows` обработан явно и переведён в доменную ошибку — [database/sql](/08-web/10-database-sql/).
- Транзакция: `BeginTx`, сразу `defer tx.Rollback()`, все запросы через `tx`, а не через `db` — [транзакции](/10-services-db/06-transactions/).
- Транзакция передаётся явно (`DBTX`), а не прячется в контексте или глобальной переменной — [транзакции](/10-services-db/06-transactions/).
- Только плейсхолдеры (`?`, `$1`); имена колонок и направление сортировки — из allowlist — [SQL-инъекции](/09-security/03-sql-injection/).
- Таймауты запросов идут через `context`; у долгих — свой дедлайн — [ошибки, context и остановка](/10-services-db/03-errors-context-shutdown/).
- Нет N+1: данные для списка загружаются одним-двумя запросами (`IN`/`= ANY($1)`, `JOIN`), вставки — пачками — [N+1 и батчи](/10-services-db/09-n-plus-one-batches/).
- Схема меняется миграциями, совместимыми с работающей версией (expand/contract); SQL проверяется `sqlc` или тестами на базе — [миграции и sqlc](/10-services-db/08-migrations-sqlc/).
- Событие для брокера пишется в outbox в той же транзакции, что и изменение данных — [outbox](/10-services-db/10-outbox/).
- Ошибки СУБД классифицируются по коду (`*pgconn.PgError.Code`, `*mysql.MySQLError.Number`), а не по тексту — [PostgreSQL через pgx](/10-services-db/11-postgres-pgx/), [MySQL](/10-services-db/14-mysql/).
- Новые запросы к большим таблицам проверены `EXPLAIN (ANALYZE)`, под них есть индексы — [индексы и EXPLAIN](/10-services-db/13-postgres-indexes-explain/).

## Тесты

- Тесты проверяют поведение через публичный API, а не порядок вызовов зависимостей — [тесты в промышленном проекте](/07-testing/09-testing-practices/).
- Table-driven тесты с подтестами; сообщение называет вызов, `got` и `want` — [table-driven тесты](/07-testing/02-table-driven/).
- Хелперы помечены `t.Helper()`, ресурсы убираются через `t.Cleanup`, временные файлы — в `t.TempDir()` — [хелперы и фикстуры](/07-testing/03-helpers-and-fixtures/).
- Нет `time.Sleep` для синхронизации; время и случайность внедряются — [параллельные тесты и synctest](/07-testing/04-parallel-and-synctest/), [тесты в промышленном проекте](/07-testing/09-testing-practices/).
- Зависимости подменяются фейками за маленьким интерфейсом; фейк проходит тот же контрактный набор, что и настоящая реализация — [тестируемый сервис](/10-services-db/04-testable-service/), [моки и httptest](/07-testing/08-mocks-and-httptest/).
- SQL проверяется на базе (SQLite в памяти, транзакция на тест, PostgreSQL по DSN с `t.Skip`), а не моком — [тестируемый сервис](/10-services-db/04-testable-service/).
- Тесты не делят состояние: своя база/транзакция/хранилище на тест, `-shuffle=on` не ломает набор — [тестируемый сервис](/10-services-db/04-testable-service/), [тесты в промышленном проекте](/07-testing/09-testing-practices/).
- HTTP-обработчики тестируются через `httptest`, включая коды ошибок и тело ответа — [моки и httptest](/07-testing/08-mocks-and-httptest/).
