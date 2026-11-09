---
title: protovalidate — правила в контракте
summary: "Валидация, записанная в .proto: правила buf.validate для строк, чисел, repeated, oneof и сообщений, CEL-выражения, protovalidate.New и Validate, разбор ValidationError, перевод в InvalidArgument с errdetails.BadRequest и проверка в interceptor сервера."
difficulty: 4
minutes: 40
runner:
  race: true
prerequisites:
  - 11-grpc/01-protobuf
  - 11-grpc/03-buf
  - 08-web/06-validation
  - 09-security/02-input-validation
docs:
  - title: "protovalidate: документация"
    url: https://protovalidate.com/
  - title: "Пакет buf.build/go/protovalidate"
    url: https://pkg.go.dev/buf.build/go/protovalidate
  - title: "Google AIP-193: ошибки и BadRequest"
    url: https://google.aip.dev/193
---

Правила вроде «email корректный», «от 1 до 20 позиций», «количество от 1 до 100» обычно живут в коде сервиса — и только там. Клиент на другом языке о них не знает, документация отстаёт, второй сервис с тем же сообщением проверяет по-своему. **protovalidate** переносит правила в контракт: это опции `buf.validate` на полях и сообщениях, а библиотеки для Go, Java, Python, C++ и TypeScript читают их из дескриптора и проверяют одинаково. В Go это `buf.build/go/protovalidate`; генерировать ничего не нужно — правила уже лежат в дескрипторе, который вкомпилирован в `*.pb.go`.

Подключение — импорт `buf/validate/validate.proto` (зависимость `buf.build/bufbuild/protovalidate` в `buf.yaml`, в песочнице она уже есть). Правила:

- **На поле** — по типу: `(buf.validate.field).string` (`min_len`, `max_len`, `pattern`, `email`, `uuid`, `uri`, `in`), `.int64`/`.uint32`/… (`gt`, `gte`, `lt`, `lte`, `in`, `const`), `.repeated` (`min_items`, `max_items`, `unique`, `items` — правило на каждый элемент), `.map` (`keys`, `values`), `.enum` (`defined_only`), `.timestamp`, `.duration`.
- **`required`** — поле обязано быть задано. Для поля-сообщения и `optional` — «есть presence», для обычного скаляра proto3 — «не нулевое значение»: `required` у `string city` значит «не пустая».
- **`(buf.validate.oneof).required = true`** — выбран ровно один вариант `oneof` (без этого `oneof` может быть пустым).
- **CEL** — выражение на языке Common Expression Language там, где готовых правил мало. На поле (`(buf.validate.field).cel`) `this` — значение поля: `this % 100 == 0`. На сообщении (`(buf.validate.message).cel`) `this` — всё сообщение, так пишут правила между полями: «скидка больше 10% — только с купоном». У CEL-правила свои `id` и `message`.
- Вложенные сообщения проверяются рекурсивно: правила `Item` применяются к каждому элементу `items`, путь нарушения — `items[0].quantity`.

В коде — один валидатор на процесс: `protovalidate.New()` (или глобальный `protovalidate.Validate`). Он безопасен для конкурентного использования, а правила типа компилирует при первой проверке и кеширует. `v.Validate(msg)` возвращает:

- `nil` — сообщение корректно;
- `*protovalidate.ValidationError` — нарушения: `Violations []*Violation`, у каждого `Proto` (сообщение `buf.validate.Violation`: `GetField()` — путь, `GetRuleId()` — id правила вроде `string.email` или `order.discount_needs_coupon`, `GetMessage()` — текст). Путь в строку переводит `protovalidate.FieldPathString` (`items[0].sku`);
- `*protovalidate.CompilationError` или `*protovalidate.RuntimeError` — ошибка в самих правилах (CEL ссылается на несуществующее поле, выражение не того типа). Это баг сервера, а не клиента.

`WithFailFast()` останавливает проверку на первом нарушении — дешевле, но клиент узнаёт об ошибках по одной.

На gRPC нарушения отдают кодом `codes.InvalidArgument` с деталями `google.rpc.BadRequest` (пакет `errdetails`): по `FieldViolation` на нарушение — `Field` (путь), `Description` (текст), `Reason` (машинный id правила). Клиент по деталям подсвечивает поля формы, а не разбирает строку ошибки. Детали прикрепляются `status.New(...).WithDetails(...)` и читаются `status.Convert(err).Details()`; подробно про статусы и детали — урок 08.

Проверять каждый метод руками незачем: **interceptor** сервера (подробно — урок 09) получает каждый запрос до обработчика и проверяет его один раз для всех методов. Обработчики видят только корректные запросы.

Валидировать на сервере нужно, даже если клиент проверяет то же самое: клиент — граница доверия по другую сторону (модуль 9), его можно обойти `grpcurl`-ом или старой версией приложения. Клиентская проверка — ради удобства пользователя, серверная — ради корректности данных. Правила в контракте делают обе одинаковыми.

## Как это в C#

- FluentValidation и DataAnnotations — правила в коде C#-классов, их видит только .NET. protovalidate — правила в `.proto`: один источник для сервера на Go, фронтенда на TypeScript и клиентов на других языках (для .NET есть реализации от сообщества).
- `AbstractValidator<T>` с `RuleFor(x => x.Email).EmailAddress()` ↔ `[(buf.validate.field).string.email = true]`; `Must(...)` и правила между свойствами ↔ CEL на сообщении. `ValidationResult.Errors` с `PropertyName` ↔ `Violations` с путём поля.
- В Grpc.AspNetCore проверку ставят в `Interceptor` (`UnaryServerHandler`) и бросают `RpcException` со статусом `InvalidArgument`; детали `BadRequest` прикрепляет пакет `Grpc.StatusProto` (`ToRpcException()`). В Go — `status.Error` как возвращаемое значение, без исключений.
- В ASP.NET Core MVC `[ApiController]` проверяет модель автоматически и отдаёт 400 с `ValidationProblemDetails`. В grpc-go автоматики нет: interceptor с protovalidate ставится явно при создании сервера — `grpc.NewServer(grpc.UnaryInterceptor(...))`.

## Ловушки

### `required` у скаляра без presence

```proto
uint32 discount_percent = 5 [(buf.validate.field).required = true];
```

У обычного скаляра proto3 «не задано» и «ноль» неотличимы, поэтому `required` здесь значит «не ноль» — и скидка 0% станет ошибкой валидации. Если ноль допустим, а отсутствие — нет, объявите поле `optional`: тогда `required` проверяет presence. То же со строкой: `required` у `string` запрещает пустую строку.

### Ошибка в правиле — не вина клиента

```go
err := v.Validate(req)
if err != nil {
	return nil, status.Error(codes.InvalidArgument, err.Error()) // и для CompilationError
}
```

CEL-выражение с опечаткой (`this.no_such_field > 0`) даёт `*protovalidate.CompilationError` на каждом запросе. Отвечать на него `InvalidArgument` — значит обвинить клиента в баге сервера, да ещё и отдать ему текст ошибки компиляции с именами внутренних полей. Различайте `*ValidationError` (→ `InvalidArgument` с деталями) и всё остальное (→ `Internal`, подробности — в лог). Ошибки в правилах ловит до выкатки правило `PROTOVALIDATE` в `buf lint`.

### Порядок нарушений в ответе

```go
for _, v := range verr.Violations {
	details = append(details, toFieldViolation(v)) // порядок — как обошёл валидатор
}
```

Порядок обхода зависит от устройства валидатора и может поменяться с его версией. Клиентские тесты и снимки ответов, которые сравнивают детали целиком, начнут падать без изменений в контракте. Сортируйте нарушения перед ответом — по пути поля и id правила.

## Задание

Напишите interceptor валидации для gRPC-сервера. Контракт каталога с правилами — `catalog.proto` рядом с тестами; тесты поднимают сервер на `bufconn` с вашим interceptor'ом.

```go
func UnaryValidator(v protovalidate.Validator) grpc.UnaryServerInterceptor
```

- Запрос, который не реализует `proto.Message`, передаётся обработчику без проверки.
- `v.Validate(req)` вернул `nil` — вызывается обработчик, его ответ и ошибка возвращаются без изменений.
- `v.Validate` вернул `*protovalidate.ValidationError` (ищите через `errors.As`) — обработчик **не** вызывается, ответ `nil`, ошибка — статус `codes.InvalidArgument` с сообщением ровно `invalid request` и ровно одной деталью `*errdetails.BadRequest`. В ней по `FieldViolation` на каждое нарушение: `Field` — `protovalidate.FieldPathString(viol.Proto.GetField())` (у правила уровня сообщения — пустая строка), `Description` — `viol.Proto.GetMessage()`, `Reason` — `viol.Proto.GetRuleId()`. Нарушения отсортированы по `Field`, затем по `Reason`, затем по `Description` (строки по возрастанию). Если `WithDetails` вернул ошибку — ответ как в следующем пункте (`codes.Internal`, `internal error`).
- Любая другая ошибка `Validate` (ошибка компиляции или выполнения правил) — обработчик не вызывается, ответ `nil`, статус `codes.Internal` с сообщением ровно `internal error`, без деталей.
