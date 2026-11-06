# Go для C#-разработчика

Интерактивный учебник по Go для разработчиков с опытом C#/.NET: уроки с аннотированными примерами, блоком «Как это в C#», ловушками, квизами и задачами с автотестами.

## Структура

| Путь | Что там |
| --- | --- |
| `content/` | Уроки: папка на урок с настоящими `.go`-файлами. Формат — [content/README.md](content/README.md) |
| `tools/lessonlint/` | Проверка структуры и прогон каждого урока |
| `site/` | Сайт на Astro, собирается из `content/` |
| `runner/` | Сервис исполнения кода: `/api/run`, `/api/test`, `/api/fmt` |
| `viz/` | Движок визуализаций (этап 4) |

## Запуск в Docker

```sh
docker compose up --build        # учебник на http://localhost:8080
```

- `site` — nginx со статикой Astro, `/api/*` проксируется в `runner`.
- `runner` — Go-тулчейн с прогретым кешем сборки. Каждый запуск идёт в отдельном временном каталоге от имени отдельного uid слота с лимитами: 10 с CPU и реального времени, 256 МБ, 64 процесса, вывод до 64 КБ. После запуска все процессы слота убиваются.
- У `runner` нет выхода в интернет (сеть `internal`); модули — только из кеша образа.
- В `site/package-lock.json` записаны адреса корпоративного Artifactory. Образ сайта ставит пакеты из `registry.npmjs.org`, подменяя префикс через `--replace-registry-host`; integrity-хеши проверяются как обычно. Другой реестр: `docker compose build --build-arg NPM_REGISTRY=... site`.
- Режим `server` (runner внутри gVisor): `docker compose -f docker-compose.yml -f docker-compose.server.yml up -d`, нужен runtime `runsc`.

### API runner

| Запрос | Тело | Ответ |
| --- | --- | --- |
| `POST /api/run` | `{"lesson"?, "files": {"main.go": "..."}, "flags"?}` | `stdout`, `stderr`, `exitCode`, `timedOut`, `truncated`, `buildFailed`, `buildOutput`, `durationMs` |
| `POST /api/test` | `{"lesson", "files": {"task.go": "..."}}` | `passed`, `buildFailed`, `tests: [{name, status, output, elapsed, hidden}]`, `durationMs` |
| `POST /api/fmt` | `{"files": {...}}` | `files`, `errors` |
| `POST /api/solution` | `{"lesson"}` | `files` — эталон; сайт показывает его после успеха или трёх неудачных попыток |

Тесты для `/api/test` runner берёт из контента урока, включая `hidden_test.go`; клиент присылает только своё решение.

## Команды

```sh
# проверить весь контент: пример, эталон, шаблон задачи, квизы
go run ./tools/lessonlint content

# только один урок; -update перезаписывает example/output.txt
go run ./tools/lessonlint -run 05-concurrency/07-select -update content

# сайт: /api/* в dev-режиме проксируется в runner на localhost:8081
go run ./runner                      # runner без изоляции, для разработки
cd site && npm ci && npm run dev     # http://localhost:4321
cd site && npm run build             # статика в site/dist

# тесты сайта: логика прогресса и «Поделиться»
cd site && npm test
# e2e: уроки от начала до конца в браузере, против docker compose на :8080
cd site && npx playwright install chromium && npm run e2e
```

Прогресс (квизы, попытки, «пройдено») хранится в `localStorage`; на главной есть экспорт и импорт JSON. Черновики кода тоже лежат в `localStorage`, но в экспорт не попадают.

```sh
# тесты runner; изоляция по uid и тесты fork-бомбы — только под root на Linux
go test ./runner/...
docker run --rm --init -v "$PWD:/repo" -w /repo/runner -e GOWORK=off go-tutor/runner:dev go test ./...
```

Для разработки без Docker нужны Go 1.25+ и Node 24.
