---
title: buf — инструмент для контрактов
summary: "buf вместо protoc: buf.yaml (модули, deps, lint, breaking), buf.gen.yaml и плагины, buf.lock, buf generate, buf lint (MINIMAL/BASIC/STANDARD, правила и except), buf breaking (FILE/PACKAGE/WIRE_JSON/WIRE), BSR, buf format и проверка контрактов в CI."
difficulty: 3
minutes: 35
buf:
  expectLint:
    - RPC_REQUEST_STANDARD_NAME
    - FIELD_LOWER_SNAKE_CASE
    - ENUM_ZERO_VALUE_SUFFIX
    - ENUM_VALUE_PREFIX
prerequisites:
  - 11-grpc/01-protobuf
  - 11-grpc/02-contract-evolution
docs:
  - title: "buf: конфигурация buf.yaml v2"
    url: https://buf.build/docs/configuration/v2/buf-yaml/
  - title: "buf lint: правила и категории"
    url: https://buf.build/docs/lint/rules/
  - title: "buf breaking: правила и категории"
    url: https://buf.build/docs/breaking/rules/
---

Классический путь — `protoc` с плагинами: компилятор ставят отдельно, каждому плагину — свой флаг и путь, `-I` для импортов, а `googleapis` копируют в репозиторий руками. Стиль и совместимость никто не проверяет. **buf** заменяет всё это одним бинарником: свой компилятор `.proto`, генерация по конфигу, зависимости с lock-файлом, линтер и проверка ломающих изменений. Это фактический стандарт для Go-проектов с gRPC.

Типичный репозиторий:

```
proto/
  shop/orders/v1/orders.proto   # пакет shop.orders.v1 — каталог совпадает с пакетом
  shop/stock/v1/stock.proto
buf.yaml                        # модули, зависимости, lint, breaking
buf.gen.yaml                    # что и куда генерировать
buf.lock                        # точные версии зависимостей
gen/go/shop/orders/v1/...       # сгенерированный код (коммитится)
```

**`buf.yaml`** (версия v2) описывает модули — каталоги с `.proto` (`modules: [{path: proto}]`), зависимости `deps` (например, `buf.build/googleapis/googleapis` ради `google/api/annotations.proto` и `buf.build/bufbuild/protovalidate`), правила `lint` и `breaking`. **`buf.lock`** фиксирует коммит каждой зависимости, как `go.sum`; обновляет его `buf dep update`. Зависимости живут в **BSR** (Buf Schema Registry, `buf.build`) — реестре контрактов, куда модули публикуют командой `buf push`. BSR же собирает из модулей готовые пакеты для языков: Go-код правил protovalidate, который вы импортируете в уроке 04, приходит из `buf.build/gen/go/bufbuild/protovalidate/...` — это сгенерированный BSR модуль, а не написанная руками библиотека.

**`buf.gen.yaml`** перечисляет плагины: `local: protoc-gen-go` (программа из `PATH`) или `remote: buf.build/protocolbuffers/go` (плагин выполняется на BSR), каталог `out` и опции — `paths=source_relative` кладёт `orders.pb.go` рядом с путём `.proto`, а не по полному `go_package`. **Managed mode** (`managed: {enabled: true}` и `go_package_prefix`) вычисляет `go_package` сам, и в `.proto` не приходится писать Go-специфичные опции. `buf generate` компилирует модуль и вызывает плагины: `protoc-gen-go` пишет сообщения (`*.pb.go`), `protoc-gen-go-grpc` — интерфейсы сервера и клиента (`*_grpc.pb.go`), `protoc-gen-grpc-gateway` — REST-обработчики (урок 11).

В песочнице `buf.gen.yaml` и `buf.lock` задаёт runner и ваши игнорирует. Причина — безопасность и отсутствие сети: плагин генерации — это программа, которую buf запускает от имени runner, и разрешить ученику указать любую программу значило бы дать ему выполнить её вне песочницы Go; а зависимости доступны только те, что лежат в кеше образа. Свой `buf.yaml` можно — но только с `version`, `deps` (из двух доступных) и правилами `lint`/`breaking`.

### buf lint

Категории вкладываются друг в друга: **MINIMAL** — то, без чего сборка разных языков ломается (один пакет на каталог, каталог совпадает с пакетом, нет циклов импорта); **BASIC** — плюс стиль имён (`FIELD_LOWER_SNAKE_CASE`, `MESSAGE_PASCAL_CASE`, `ENUM_VALUE_UPPER_SNAKE_CASE`, `ENUM_FIRST_VALUE_ZERO`, `IMPORT_USED`); **STANDARD** (по умолчанию) — плюс соглашения для эволюции и gRPC: `ENUM_ZERO_VALUE_SUFFIX` (`_UNSPECIFIED`), `ENUM_VALUE_PREFIX`, `PACKAGE_VERSION_SUFFIX` (пакет кончается на `v1`, `v1beta1`), `RPC_REQUEST_STANDARD_NAME` и `RPC_RESPONSE_STANDARD_NAME` (`GetOrderRequest`/`GetOrderResponse`), `RPC_REQUEST_RESPONSE_UNIQUE` (у каждого метода свои сообщения), `SERVICE_SUFFIX`, `FILE_LOWER_SNAKE_CASE`, `PROTOVALIDATE` (правила валидации корректны). Отдельно подключаются `COMMENTS` (у всего есть комментарий) и `UNARY_RPC` (запрет стримов).

Зачем отдельные сообщения на каждый метод, даже если сейчас `GetOrder` принимает только `id`? Через год в запрос добавится `read_mask`, а в ответ — метаданные; если метод принимает общий `OrderRef`, новое поле появится во всех методах, которые его используют. Своё сообщение у каждого метода — дешёвая страховка эволюции.

Исключения — `except` (правило выключено целиком), `ignore` (пути файлов), `ignore_only` (правило для путей) и комментарий `// buf:lint:ignore ENUM_VALUE_PREFIX` над элементом. В `buf.yaml` примера выключены два правила с объяснением — так делают команды, живущие по Google AIP, где `GetOrder` возвращает сам `Order`.

### buf breaking

Сравнивает текущий модуль с прежней версией (каталог, архив, ветка git, модуль BSR) по правилам выбранной категории:

- **FILE** (по умолчанию) — самая строгая: ломающим считается всё, что ломает сгенерированный код хоть в одном языке, включая перенос сообщения в другой файл, переименование поля (`FIELD_SAME_NAME`), удаление поля даже с `reserved` (`FIELD_NO_DELETE`), смену `go_package`.
- **PACKAGE** — то же, но типы можно переносить между файлами одного пакета.
- **WIRE_JSON** — только то, что ломает бинарный формат или JSON: удаление поля без резервирования номера и имени, смена типа, переименование (для JSON имя важно).
- **WIRE** — только бинарный формат: переименования разрешены, `int32` → `int64` тоже.

Публичное API, которое генерируют внешние клиенты, держат на FILE. WIRE подходит, если все клиенты — свои и собираются из одного репозитория вместе с контрактом.

### buf format и CI

`buf format -w` приводит файлы к единому виду (как `gofmt`), `buf format -d --exit-code` в CI падает на неотформатированном. Проверка контракта в CI — три команды:

```sh
buf lint
buf format -d --exit-code
buf breaking --against '.git#branch=main'   # или '.git#branch=main,subdir=proto'
```

Для GitHub есть готовое действие `bufbuild/buf-action`: оно же комментирует нарушения в pull request.

Контракт примера нарушает четыре правила нарочно. Нажмите «buf lint», прочитайте вывод (`файл:строка:столбец ПРАВИЛО сообщение`) и исправьте контракт: переименуйте поле, добавьте `ORDER_STATUS_UNSPECIFIED`, префикс у `PAID`, сообщение `GetOrderRequest`. Пример на Go печатает дескриптор контракта, с которым работает buf, и повторяет одно его правило вручную — `ENUM_VALUE_PREFIX`.

## Как это в C#

- `Grpc.Tools` в .NET — это `protoc` и плагин C#, встроенные в MSBuild: `<Protobuf Include="..." GrpcServices="Server" />`, генерация на каждой сборке. Ни линтера, ни проверки совместимости там нет; buf работает с любым языком, и в смешанном Go/C#-репозитории одна `buf lint` проверяет общий контракт, а C# дальше собирается своим `Grpc.Tools` (или `buf generate` с плагином `buf.build/grpc/csharp`).
- `buf.lock` и BSR ↔ `packages.lock.json` и NuGet: зависимости фиксируются по хешу, а реестр раздаёт и исходники контрактов, и сгенерированные пакеты (как NuGet-пакет с готовыми классами).
- `buf lint` ↔ анализаторы Roslyn с `.editorconfig`: категории правил, исключения по правилу и подавление комментарием (`#pragma warning disable` ↔ `// buf:lint:ignore`).
- `buf breaking` ближе всего к Microsoft.DotNet.ApiCompat (проверка совместимости публичного API сборки): он сравнивает «до» и «после» и падает на ломающем изменении — только не для сборки, а для контракта на проводе.

## Ловушки

### Общие сообщения запроса и ответа

```proto
rpc GetOrder(OrderRef) returns (Order);
rpc CancelOrder(OrderRef) returns (Order);
```

Сегодня обоим методам нужен только `id`. Завтра `CancelOrder` нужна причина отмены — поле `reason` появится и в `GetOrder`, где оно ничего не значит, а клиенты начнут его заполнять. Разделить сообщения потом — ломающее изменение. buf ловит это правилами `RPC_REQUEST_STANDARD_NAME` и `RPC_REQUEST_RESPONSE_UNIQUE`: заводите `GetOrderRequest` и `CancelOrderRequest` с первого дня, даже одинаковые.

### `except` вместо исправления

```yaml
lint:
  use: [STANDARD]
  except: [ENUM_ZERO_VALUE_SUFFIX, FIELD_LOWER_SNAKE_CASE, PACKAGE_VERSION_SUFFIX]
```

Удобно заглушить замечания, но каждое правило STANDARD защищает эволюцию: без `_UNSPECIFIED` не отличить «не задано» от первого статуса, без версии в пакете не выпустить `v2` рядом с `v1`. Выключайте правило, только если у команды есть осознанная альтернатива (как AIP-стиль в примере), и пишите причину комментарием рядом.

### `buf breaking` против рабочей копии

```sh
buf breaking --against '.git#branch=main'
```

В CI ветку `main` нужно иметь локально: `actions/checkout` по умолчанию клонирует один коммит без других веток, и сравнение падает с ошибкой, что ветка не найдена (нужен `fetch-depth: 0` или явный `git fetch origin main`). Сравнивать надо с тем, что уже выпущено: с `main` или с тегом релиза. Сравнение с предыдущим коммитом той же ветки пропустит ломающее изменение, если оно пришло двумя коммитами.

## Задание

Напишите мини-линтер — четыре правила buf по дескриптору файла, без запуска buf. Дескриптор файла даёт сгенерированный код (`File_lintme_proto`); тесты строят и свои дескрипторы.

```go
const (
	RuleFieldLowerSnakeCase     = "FIELD_LOWER_SNAKE_CASE"
	RuleEnumZeroValueSuffix     = "ENUM_ZERO_VALUE_SUFFIX"
	RuleRPCRequestStandardName  = "RPC_REQUEST_STANDARD_NAME"
	RuleRPCResponseStandardName = "RPC_RESPONSE_STANDARD_NAME"
)

type Issue struct {
	Rule    string
	Element protoreflect.FullName
}

func Lint(fd protoreflect.FileDescriptor) []Issue
```

Проверяются только элементы, объявленные в самом `fd` (импортированные файлы — нет), включая вложенные сообщения и enum на любой глубине.

- **`FIELD_LOWER_SNAKE_CASE`** — у каждого поля каждого сообщения (поля `oneof` и map — тоже) имя соответствует `^[a-z][a-z0-9]*(_[a-z0-9]+)*$`. `Element` — полное имя поля: `shop.lint.v1.Order.labelMap`.
- **`ENUM_ZERO_VALUE_SUFFIX`** — у каждого enum первое по порядку объявления значение (`Values().Get(0)`; в proto3 оно всегда равно 0) оканчивается на `_UNSPECIFIED`; одно `UNSPECIFIED` без подчёркивания — нарушение. `Element` — полное имя этого значения: значения enum живут в области видимости, где объявлен enum, поэтому это `shop.lint.v1.Order.STATUS_NEW`, без имени enum.
- **`RPC_REQUEST_STANDARD_NAME`** — у каждого метода каждого сервиса короткое имя типа запроса (`Input().Name()`) равно `<Метод>Request` или `<Сервис><Метод>Request`: для `OrderService.GetOrder` подходят `GetOrderRequest` и `OrderServiceGetOrderRequest`. Исключений нет, `google.protobuf.Empty` — тоже нарушение. `Element` — полное имя метода: `shop.lint.v1.OrderService.GetOrder`.
- **`RPC_RESPONSE_STANDARD_NAME`** — то же для типа ответа (`Output().Name()`) с суффиксом `Response`.

Результат отсортирован по `Element` (байтовое сравнение строк), при равенстве — по `Rule`. Нет нарушений — пустой результат (длины 0).
