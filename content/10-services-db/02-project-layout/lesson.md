---
title: Раскладка проекта и сборка в main
summary: cmd/ и internal/, пакеты по смыслу вместо слоёв, интерфейсы у потребителя, ручная сборка зависимостей в тестируемой функции run, конфигурация из env и флагов с проверкой при старте и без утечки секретов.
difficulty: 3
minutes: 35
prerequisites:
  - 01-start/05-modules-and-packages
  - 06-stdlib/18-command-line
  - 06-stdlib/19-environment
  - 08-web/08-graceful-shutdown
  - 10-services-db/01-idiomatic-review
docs:
  - title: "Organizing a Go module"
    url: https://go.dev/doc/modules/layout
  - title: "cmd/go: Internal Directories"
    url: https://pkg.go.dev/cmd/go#hdr-Internal_Directories
  - title: "Пакет flag"
    url: https://pkg.go.dev/flag
runner:
  race: true
---

В Go нет шаблона решения, как `dotnet new webapi`, и нет обязательной структуры каталогов. Есть одно правило компилятора и несколько договорённостей, которых держится большинство сервисов. Типичный сервис заказов (так устроен и учебный сервис `labs/orders` из урока 15):

```
orders/                        модуль gotutor/labs/orders
  go.mod
  cmd/
    orders/main.go             бинарник: только сборка зависимостей и запуск
  internal/
    orders/                    домен: Order, Service, ошибки, интерфейс Store
      service.go
      service_test.go          тесты сервиса на фейке
    store/postgres/            реализация Store на pgx
    store/sqlite/              ещё одна реализация
    storetest/                 общий контрактный набор тестов для хранилищ
    httpapi/                   HTTP-обработчики поверх orders.Service
  migrations/                  SQL-миграции
```

- **`cmd/<имя>/main.go`** — по каталогу на бинарник (`go build ./cmd/orders` даст `orders`). В `main` нет логики: прочитать конфигурацию, собрать зависимости, запустить, дождаться сигнала.
- **`internal/`** — единственное правило, которое проверяет компилятор: пакет `a/b/internal/c` могут импортировать только пакеты из дерева `a/b/...`. Всё, что лежит в `orders/internal`, недоступно другим модулям, поэтому его можно менять без оглядки на внешних пользователей. Для сервиса почти весь код — `internal`.
- **Пакеты по смыслу.** `orders`, `stock`, `billing` — каждый пакет отвечает на вопрос «что он делает», а не «к какому слою относится». Раскладка `models/`, `services/`, `repositories/`, перенесённая из .NET, в Go рождает пакеты, которые импортируют друг друга по кругу (компилятор запрещает циклы), и вызовы вида `models.Order` и `services.OrderService`. Пакеты `utils`, `common`, `helpers` — тот же запах: имя ничего не говорит, и туда сваливается всё. Каталог `pkg/` для сервиса не нужен — он имеет смысл лишь для кода, который действительно предлагается другим модулям.
- **Интерфейсы у потребителя.** `orders.Service` объявляет `type Store interface { ... }` с нужными ему методами; `store/postgres` возвращает конкретный `*Store` и про интерфейс не знает. Зависимости идут к домену: `postgres` импортирует `orders` ради типов, `orders` не импортирует никого из своих реализаций.

**Сборка вручную.** Контейнера зависимостей нет — и он не нужен: граф сервиса собирается в `main` десятком строк, в порядке, который видно глазами, а ошибка (забыли зависимость) — это ошибка компиляции, а не исключение при первом запросе. Удобный приём — вынести всё из `main` в `run(ctx, args, getenv, stdout) error`: аргументы, окружение и вывод приходят параметрами, вместо `os.Exit` возвращается ошибка, и тест вызывает `run` как обычную функцию. Сам `main` остаётся таким:

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

**Конфигурация** — одна структура: значения по умолчанию в коде, поверх них переменные окружения (в контейнерах конфигурацию задают ими), поверх — флаги (удобно локально). Проверяются все поля сразу, при старте, и при ошибке сервис не стартует — fail fast: лучше упасть при деплое с понятным сообщением, чем через час на первом запросе к неправильно настроенной базе. Сообщение называет настройку и проблему, но не повторяет значение секрета. Конфигурацию полезно напечатать при старте — с замаскированными секретами. Пример собирает всё это в одном файле: конфигурацию, хранилище, сервис, HTTP-сервер и остановку по сигналу.

## Как это в C#

- **Решение и проекты** (`Orders.Api`, `Orders.Domain`, `Orders.Infrastructure`) → один модуль и пакеты внутри. Модификатор `internal` в C# ограничивает сборку; каталог `internal/` в Go ограничивает поддерево пакетов. Отдельные сборки ради слоёв не нужны.
- **`Microsoft.Extensions.DependencyInjection`** → вызовы конструкторов в `run`. `AddSingleton<IOrderStore, PgOrderStore>()` превращается в `store := postgres.New(pool)`, а время жизни «scoped» — в аргументы запроса (`ctx`, транзакция).
- **`IConfiguration` + `IOptions<T>` + `ValidateOnStart`** → структура `Config`, `getenv` и `flag.FlagSet`, функция проверки. Источники и их приоритет задаёт ваш код, а не порядок `AddJsonFile`/`AddEnvironmentVariables`.
- **Generic Host** (`IHostedService`, `ApplicationStopping`) → `signal.NotifyContext`, горутины с контекстом и `srv.Shutdown` в `run` — урок 03 модуля.

## Ловушки

### `internal/` не в том месте

```
shop/
  cmd/shop/main.go           import "example.com/shop/orders/internal/pricing" — ошибка
  orders/internal/pricing/
```

Компилятор ответит `use of internal package example.com/shop/orders/internal/pricing not allowed`: `internal` открыт только поддереву своего родителя, то есть `orders/...`. Код, нужный нескольким частям сервиса, кладут в `internal/` в корне модуля.

### Конфигурация из глобального `flag` и `os.Getenv` по всему коду

```go
var maxConns = flag.Int("max-conns", 10, "") // в пакете store

func (s *Store) open() { if os.Getenv("DEBUG") == "1" { /* ... */ } }
```

Пакетные флаги и чтение окружения в глубине кода делают конфигурацию невидимой: какие настройки есть, где их значения по умолчанию и проверки — не найти, а тесты начинают зависеть от окружения процесса и друг от друга. Читайте всё в одном месте (`loadConfig`), передавайте значения конструкторам явно, а `os.Getenv` подменяйте параметром `getenv func(string) string`.

### Секрет в `%+v`

```go
type Config struct {
	Addr string
	dsn  secret // у secret есть String() "***"
}
log.Printf("config: %+v", cfg) // dsn напечатан целиком
```

`fmt` вызывает `String` у вложенных полей, только если поле экспортировано; неэкспортированное печатается через `reflect` как есть. Надёжнее определить `String()` (и `LogValue()` для `slog`) у всей структуры конфигурации и маскировать секреты там — например, `(*url.URL).Redacted()` заменяет пароль в URL на `xxxxx`.

## Задание

Напишите загрузку конфигурации сервиса заказов из флагов и окружения с проверкой при старте.

```go
type Config struct {
	Addr        string
	DSN         string
	MaxConns    int
	ReadTimeout time.Duration
	Debug       bool
}

var ErrInvalidConfig = errors.New("invalid config")

type FieldError struct {
	Key     string // имя переменной окружения настройки
	Problem string
}

func (e *FieldError) Error() string                 // дано
func (e *FieldError) Is(target error) bool          // дано: true для ErrInvalidConfig

func LoadConfig(args []string, getenv func(string) string) (Config, error)
func (c Config) String() string
```

Настройки — поле, переменная окружения, флаг, значение по умолчанию и правило проверки:

- `Addr` — `ORDERS_ADDR`, `-addr`, по умолчанию `:8080`; верно, если `net.SplitHostPort` разбирает значение, а порт — целое `0..65535`;
- `DSN` — `ORDERS_DSN`, `-dsn`, по умолчанию пусто; верно, если не пусто, `url.Parse` без ошибки, схема `postgres` или `postgresql` и `u.Host` не пуст;
- `MaxConns` — `ORDERS_MAX_CONNS`, `-max-conns`, по умолчанию `10`; верно в диапазоне `1..100`;
- `ReadTimeout` — `ORDERS_READ_TIMEOUT`, `-read-timeout`, по умолчанию `5s`; верно, если больше `0` и не больше `1m`;
- `Debug` — `ORDERS_DEBUG`, `-debug`, по умолчанию `false`; проверки нет.

`LoadConfig`:

- Порядок: значения по умолчанию → окружение → флаги (флаг перекрывает окружение). Пустое значение переменной окружения считается незаданным. Окружение читается только через `getenv`, флаги — из `args` (это аргументы без имени программы).
- Окружение разбирается `strconv.Atoi`, `time.ParseDuration`, `strconv.ParseBool`. Неразборчивое значение — проблема этой настройки (даже если флаг потом задаст своё); поле при этом остаётся прежним.
- Ошибка разбора флагов (неизвестный флаг, неверное значение) или лишний позиционный аргумент — сразу ошибка, для которой `errors.Is(err, ErrInvalidConfig)`, без остальных проверок. Для `-h`/`-help` — ошибка, для которой `errors.Is(err, flag.ErrHelp)`. Ничего не печатается.
- Иначе итоговые значения проверяются по правилам из списка. Если проблем нет — `(cfg, nil)`. Если есть — `Config{}` и `errors.Join` из `*FieldError`, по одной на проблему: сначала ошибки разбора окружения, затем ошибки проверки, внутри каждой группы — в порядке списка настроек. `Key` — имя переменной окружения настройки (и для значения из флага тоже).
- Текст ошибки не содержит значение DSN — в нём пароль.

`String` возвращает `addr=<Addr> dsn=<DSN> max_conns=<MaxConns> read_timeout=<ReadTimeout> debug=<Debug>`, где длительность и булево печатаются как `%s` и `%t`, а DSN: пустой — пустая строка; разбирается `url.Parse` со схемой `postgres`/`postgresql` — `(*url.URL).Redacted()` (пароль заменён на `xxxxx`), а если в DSN есть параметр `password` (`?password=…` — так его тоже принимают libpq и pgx), его значение заменено на `xxxxx` до `Redacted()`; иначе — `***`. Пример: `addr=:8080 dsn=postgres://app:xxxxx@db:5432/orders max_conns=10 read_timeout=5s debug=false`.
