# Go для C#-разработчика

Интерактивный учебник по Go для разработчиков с опытом C#/.NET: уроки с аннотированными примерами, блоком «Как это в C#», ловушками, квизами и задачами с автотестами.

План учебника, этапы и открытые вопросы — [docs/PLAN.md](docs/PLAN.md).

## Структура

| Путь | Что там |
| --- | --- |
| `content/` | Уроки: папка на урок с настоящими `.go`-файлами. Формат — [content/README.md](content/README.md) |
| `tools/lessonlint/` | Проверка структуры и прогон каждого урока |
| `site/` | Сайт на Astro, собирается из `content/` |
| `runner/` | Сервис исполнения кода: `/api/run`, `/api/test`, `/api/gotest`, `/api/vulncheck`, `/api/buf`, `/api/fmt`, `/api/trace`, `/api/race`, `/api/pprof` |
| `labs/orders/` | Сквозной учебный сервис «Заказы» (модуль 10 и дальше) — запускается в профиле `labs` |
| `docs/` | План учебника |
| `viz/` | Движок визуализаций: примитивы SVG, плеер шагов, построители шагов из трассы |

## Запуск в Docker

```sh
docker compose up --build                  # учебник на http://localhost:8080
docker compose --profile labs up --build   # плюс PostgreSQL, MySQL и сервис «Заказы» для модуля 10 и дальше
```

- `site` — nginx со статикой Astro, `/api/*` проксируется в `runner`. Порт открыт только на `127.0.0.1`, nginx отвечает лишь на `Host: localhost`/`127.0.0.1` (защита от DNS rebinding), а runner отвергает запросы с чужой страницы (`Sec-Fetch-Site: cross-site`, чужой `Origin`) и без `Content-Type: application/json`: runner исполняет присланный код, и чужой сайт в браузере не должен до него дотянуться.
- `runner` — Go-тулчейн с прогретым кешем сборки. Каждый запуск идёт в отдельном временном каталоге от имени отдельного uid слота с лимитами: 10 с CPU и реального времени, 256 МБ, 64 процесса, вывод до 64 КБ. После запуска все процессы слота убиваются.
- У `runner` нет выхода в интернет (сеть `internal`); модули — только из кеша образа. Сторонние модули, доступные урокам, перечислены в `runner/sandbox/deps.mod` (сейчас `golang.org/x/sync`, `golang.org/x/crypto`, SQLite `modernc.org/sqlite`, `pgx`, `go-sql-driver/mysql` и `goose`). Модули из `go.sum` примеров со своим `go.mod` тоже скачиваются при сборке образа. В образе есть `govulncheck` и снимок базы уязвимостей vuln.go.dev, который обновляется при каждой пересборке.
- pprof (`/api/pprof`): профиль записала программа ученика, поэтому `pprof` (собранный `cmd/pprof`: с Go 1.25 тулчейн не содержит его готовым) разбирает его так же, как запускается сама программа, — от имени uid слота и с теми же лимитами.
- Примеры с `.proto`: перед сборкой runner выполняет `buf generate` (buf и плагины `protoc-gen-go`, `protoc-gen-go-grpc`, `protoc-gen-grpc-gateway` — в образе, зависимости googleapis и protovalidate — в кеше buf образа). buf работает от имени runner, поэтому `buf.gen.yaml` и `buf.lock` всегда свои, присланный `buf.yaml` переписывается в каноническом виде после проверки, а `.proto` вместе — не больше 64 КиБ.
- Профиль `labs` добавляет PostgreSQL 18 и MySQL 8.4 (данные в tmpfs, после перезапуска базы пустые; порты только на localhost: 55432 и 53306, пароль `labs`) Kafka 4.3 (KRaft, `localhost:59092`; сервисам итогового проекта, а не песочнице: у брокера нет ACL, уроки используют kfake) и сервис «Заказы» на `localhost:8090`. Урокам с `runner.labs` runner на каждый запуск создаёт отдельную базу и пользователя, передаёт DSN в `POSTGRES_DSN` / `MYSQL_DSN` и удаляет их после запуска. Без профиля такие уроки подсказывают, как его включить. Программы песочницы работают в одной сети с базами, а пароли лежат в репозитории, поэтому права разведены (`labs/initdb`): runner ходит в базы ролью `runner` без прав суперпользователя, суперпользователям `postgres` и `root` вход из сети песочницы закрыт (а заблокированные записи MySQL защищены `SYSTEM_USER`, иначе роль runner с `CREATE USER` сняла бы блокировку), роль запуска видит только свою базу, а базам и их tmpfs ограничена память. Тесты `labs/orders` и `psql` с машины по-прежнему могут входить как `postgres`/`root`.
- В `site/package-lock.json` записаны адреса корпоративного Artifactory. Образ сайта ставит пакеты из `registry.npmjs.org`, подменяя префикс через `--replace-registry-host`; integrity-хеши проверяются как обычно. Другой реестр: `docker compose build --build-arg NPM_REGISTRY=... site`.
- Режим `server` (runner внутри gVisor): `docker compose -f docker-compose.yml -f docker-compose.server.yml up -d`, нужен runtime `runsc`.

### API runner

| Запрос | Тело | Ответ |
| --- | --- | --- |
| `POST /api/run` | `{"lesson"?, "files": {"main.go": "..."}, "flags"?}` | `stdout`, `stderr`, `exitCode`, `timedOut`, `truncated`, `buildFailed`, `buildOutput`, `durationMs`, а с флагом `gcflags` — `compilerOutput`: диагностика компилятора |
| `POST /api/test` | `{"lesson", "files": {"task.go": "..."}}` | `passed`, `buildFailed`, `tests: [{name, status, output, elapsed, hidden}]`, `durationMs` |
| `POST /api/gotest` | `{"lesson"?, "files": {"main.go": "...", "main_test.go": "..."}, "flags"?, "mode": "test" \| "bench" \| "fuzz" \| "cover", "fuzz"?: "FuzzXxx"}` | поля `/api/run` (в `stdout` — вывод `go test` как в терминале) плюс `coverage` — отчёт `go tool cover -func` и `fuzzInputs` — входы, на которых фаззер нашёл ошибку |
| `POST /api/vulncheck` | `{"lesson"?, "files": {"main.go": "...", "go.mod"?: "...", "go.sum"?: "..."}}` | `output` — отчёт `govulncheck ./...`, `found` — код вызывает уязвимые функции, `failed` — проверить не удалось, `dbModified` — дата снимка базы; 501, если в runner нет `govulncheck` |
| `POST /api/buf` | `{"lesson"?, "files": {"x.proto": "...", "buf.yaml"?: "..."}, "against"?: {...}}` | `buf lint`, а с `against` — `buf breaking` против прежней версии: `output` (`файл:строка:столбец ПРАВИЛО сообщение`), `found`, `failed`; 501, если в runner нет buf |
| `POST /api/fmt` | `{"files": {...}}` | `files`, `errors` |
| `POST /api/solution` | `{"lesson"}` | `files` — эталон; сайт показывает его после успеха или трёх неудачных попыток |
| `POST /api/trace` | как у `/api/run` | поля `/api/run` плюс `trace`: горутины, операторы `select` со счётчиками веток, события; при сбое — `traceError` |
| `POST /api/race` | как у `/api/run` | поля `/api/run` плюс `races`: отчёты `-race` — два доступа (вид, горутина, стек из пакета `main`) и где созданы горутины |
| `POST /api/pprof` | как у `/api/run` | поля `/api/run` плюс `profiles: [{name, top, failed}]` — `pprof -top` по каждому `*.pprof`, который программа записала в рабочий каталог (не больше 4); 501, если в runner нет pprof |

Тесты для `/api/test` runner берёт из контента урока, включая `hidden_test.go`; клиент присылает только своё решение.

`flags` — `race`, `gomaxprocs`, `godebug`, `gcflags`, `gotraceback` и `labs` (`["postgres"]`, `["mysql"]`); они накладываются на флаги урока из frontmatter. `gcflags` — только слова из белого списка (`-m`, `-m=2`, `-l`, `-N`, `-d=ssa/check_bce/debug=1`) и только для пакета программы, `gotraceback` — `none`, `single`, `all`, `system`. Если нужной базы labs нет, программа запускается без DSN, а в начало `stderr` runner дописывает подсказку.

`/api/trace` нужен живым визуализаторам:

- Runner переименовывает `main` в `gotutorMain` и добавляет файл с настоящим `main`, который включает `runtime/trace`.
- Ветки `select` размечаются вызовами на тех же строках, поэтому номера строк в трассе совпадают с редактором.
- Трассу разбирает `golang.org/x/exp/trace` на стороне runner, клиенту уходит JSON — не больше 2000 событий.
- `-race` в этом режиме выключен.
- Если программа завершилась через `os.Exit`, паникой в горутине или deadlock'ом, трасса не дописывается, и ответ содержит `traceError`.

## Команды

```sh
# проверить весь контент: пример, эталон, шаблон задачи, квизы
go run ./tools/lessonlint content

# только один урок; -update перезаписывает example/output.txt
go run ./tools/lessonlint -run 05-concurrency/07-select -update content

# уроки на настоящих базах (нужен docker compose --profile labs up) и сверка кода sqlc
go run ./tools/lessonlint -postgres 'postgres://runner:labs-runner@127.0.0.1:55432/postgres?sslmode=disable' \
  -mysql 'runner:labs-runner@tcp(127.0.0.1:53306)/' -sqlc "$(command -v sqlc)" -run 10-services-db/ content
# примеры с runtime/pprof: pprof ищется в PATH, иначе lessonlint соберёт cmd/pprof сам (или -pprof путь)
# примеры с .proto: buf и плагины protoc-gen-go, protoc-gen-go-grpc, protoc-gen-grpc-gateway в одном каталоге
go run ./tools/lessonlint -buf "$(command -v buf)" -run 11-grpc/ content

# сайт: /api/* в dev-режиме проксируется в runner на localhost:8081
go run ./runner                      # runner без изоляции, для разработки
cd site && npm ci && npm run dev     # http://localhost:4321
cd site && npm run build             # статика в site/dist

# тесты сайта: прогресс, «Поделиться», движок визуализаций и все viz.ts
# (образцы ответов runner для них: GOTUTOR_FIXTURE=1 go test ./runner/livetrace -run WriteFixture)
cd site && npm test
# e2e: уроки от начала до конца, визуализаторы и слайды — против docker compose на :8080
cd site && npx playwright install chromium && npm run e2e
```

Слайды модуля — `/slides/<модуль>/` (ссылка в оглавлении), формат в [content/README.md](content/README.md#slidesmd).

Прогресс (квизы, попытки, «пройдено») хранится в `localStorage`; на главной есть экспорт и импорт JSON. Черновики кода тоже лежат в `localStorage`, но в экспорт не попадают.

```sh
# тесты runner; изоляция по uid и тесты fork-бомбы — только под root на Linux;
# временные базы labs — при RUNNER_TEST_POSTGRES / RUNNER_TEST_MYSQL (DSN роли runner) и RUNNER_TEST_KAFKA;
# генерация из .proto и buf lint/breaking — при RUNNER_BUF (или buf в PATH)
go test ./runner/...
docker run --rm --init -v "$PWD:/repo" -w /repo/runner -e GOWORK=off go-tutor/runner:dev go test ./...
```

Для разработки без Docker нужны Go 1.25+ и Node 24.
