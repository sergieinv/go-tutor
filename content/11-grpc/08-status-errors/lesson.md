---
title: Статусы и ошибки gRPC
summary: "status.Error и status.FromError, смысл кодов и что из них повторять, детали errdetails (BadRequest, ErrorInfo, RetryInfo, PreconditionFailure), перевод доменных ошибок в одном месте и соответствие кодов HTTP."
difficulty: 4
minutes: 40
runner:
  race: true
prerequisites:
  - 11-grpc/05-grpc-unary
  - 04-errors/04-wrapping
  - 10-services-db/03-errors-context-shutdown
  - 09-security/11-secrets-logs-errors
docs:
  - title: "gRPC: Status codes"
    url: https://grpc.io/docs/guides/status-codes/
  - title: "gRPC: Error handling (richer error model)"
    url: https://grpc.io/docs/guides/error/
  - title: "Пакет grpc/status"
    url: https://pkg.go.dev/google.golang.org/grpc/status
---

Ошибка gRPC-вызова — это статус: код из 17 значений (`codes.Code`), сообщение для разработчика и необязательный список деталей. На проводе он едет в трейлерах (`grpc-status`, `grpc-message`, детали — сериализованный `google.rpc.Status` в `grpc-status-details-bin`). В Go статус — `*status.Status`: `status.New(code, msg)` создаёт его, `st.Err()` превращает в `error` для возврата из обработчика, `status.Error(code, msg)` и `status.Errorf` делают то и другое сразу. Обратно: `status.FromError(err)` возвращает статус и признак, что он там был (обёртки `%w` разворачиваются), `status.Convert(err)` — то же без признака, `status.Code(err)` — только код. Ошибка без статуса превращается в `codes.Unknown`, а её текст уходит клиенту как есть.

Код — это контракт с клиентом: по нему решают, что показать пользователю и повторять ли вызов. Самые частые и их границы:

| Код | Когда | HTTP | Повторять? |
| --- | --- | --- | --- |
| `InvalidArgument` | Запрос плох сам по себе: пустое поле, неверный формат | 400 | нет |
| `FailedPrecondition` | Запрос нормален, но состояние не то: оплатить отменённый заказ | 400 | нет, пока состояние не изменится |
| `OutOfRange` | Выход за допустимый диапазон, который может сдвинуться: страница за концом списка | 400 | нет |
| `NotFound` / `AlreadyExists` | Ресурса нет / уже есть | 404 / 409 | нет |
| `Aborted` | Конфликт конкурентных операций: оптимистичная блокировка, сериализация | 409 | да — всю операцию заново |
| `Unavailable` | Временно недоступно: перегрузка, перезапуск, сеть | 503 | да, с backoff |
| `ResourceExhausted` | Квота или лимит (в том числе размер сообщения) | 429 | только если сервер сказал когда |
| `DeadlineExceeded` / `Canceled` | Время вышло / клиент отменил | 504 / 499 | обычно нет: бюджет уже потрачен |
| `Internal` / `Unknown` | Сломано что-то внутри | 500 | нет |
| `Unauthenticated` / `PermissionDenied` | Нет или неверные учётные данные / нет прав | 401 / 403 | нет |

Колонка HTTP — так переводит `runtime.HTTPStatusFromCode` из grpc-gateway (урок 11). Обратите внимание: `FailedPrecondition` — это 400, а не 412, и gateway делает это намеренно.

Детали — сообщения protobuf, упакованные в `Any`. Стандартный набор лежит в `google.golang.org/genproto/googleapis/rpc/errdetails`: `BadRequest` (нарушения по полям — для форм), `ErrorInfo` (машиночитаемая причина `Reason` + `Domain` + метаданные — по ним клиент ветвится, не разбирая текст), `RetryInfo` (через сколько повторить), `PreconditionFailure` (какое условие нарушено), а также `QuotaFailure`, `ResourceInfo`, `LocalizedMessage`. Добавляет их `st.WithDetails(...)` — он **возвращает новый статус**, а клиент получает распакованные сообщения из `st.Details()` и разбирает их type switch'ем.

Перевод доменных ошибок в статусы делают в одном месте на краю сервиса — функцией `toStatus` и interceptor'ом, через который проходят все методы. Обработчики и слои ниже возвращают `ErrNotFound`, `*ValidationError` и т.п., ничего не зная о gRPC. Всё, что не распознано, — `codes.Internal` с нейтральным сообщением: текст исходной ошибки (SQL, адреса, имена файлов, стек) остаётся в логе вместе с id запроса. Статус, пришедший от соседнего сервиса, — тоже чужая ошибка: прозрачно пробрасывать его можно только осознанно (как в уроке 07), иначе клиент увидит `NotFound` о ресурсе, о котором он не спрашивал.

## Как это в C#

- `RpcException` со `Status(StatusCode, detail)` ↔ `status.Error`. В .NET ошибку бросают, в Go возвращают значение, а клиент вместо `catch (RpcException ex) when (ex.StatusCode == ...)` проверяет `status.Code(err)`.
- Rich error model: пакет `Grpc.StatusProto` — `Google.Rpc.Status` с `Details = { Any.Pack(...) }`, `.ToRpcException()` и `ex.GetRpcStatus()?.GetDetail<BadRequest>()` ↔ `st.WithDetails(...)` и type switch по `st.Details()`. Типы деталей те же — `google.rpc.*`, это общий контракт для всех языков.
- Необработанное исключение в Grpc.AspNetCore уходит клиенту как `Unknown` с безличным «Exception was thrown by handler.» (текст — только при `EnableDetailedErrors`). В Go безопасного умолчания нет: обычная ошибка уйдёт как `Unknown` вместе с `err.Error()`. Перевод в статус — не украшение, а защита.
- Перевод исключений в коды в .NET обычно делают interceptor'ом или middleware; в Go — то же самое: unary interceptor (урок 09), только вместо `catch` по типам — `errors.Is`/`errors.As` по цепочке обёрток.

## Ловушки

### Сырая ошибка наружу

```go
// УЯЗВИМО: клиент получит Unknown и текст вида
// "pq: password authentication failed for user "orders" at 10.0.3.7".
func (s *server) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	o, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	...
}
```

Ошибка без статуса не отбрасывается, а превращается в `Unknown` с полным текстом — схема базы, адреса, пути становятся видны любому клиенту (`09-security/11`). Возвращайте только статусы: доменные — через `toStatus`, остальное — `status.Error(codes.Internal, "internal error")`, подробности — в лог.

### Результат `WithDetails` выброшен

```go
st := status.New(codes.InvalidArgument, "invalid request")
st.WithDetails(br) // деталей у st нет
return nil, st.Err()
```

`WithDetails` работает как `append`: исходный статус не меняется. Пишите `st, err = st.WithDetails(br)` и решите, что делать с ошибкой (её не бывает на нормальных сообщениях, но бывает на коде `OK`).

### Повтор того, что не пройдёт

```go
for attempt := 0; attempt < 5; attempt++ {
	_, err = client.CreateOrder(ctx, req)
	if err == nil {
		break
	} // повторяем InvalidArgument, NotFound, DeadlineExceeded...
}
```

`InvalidArgument` и `FailedPrecondition` не пройдут ни со второй, ни с пятой попытки, а нагрузку умножат. Повторять стоит `Unavailable`, `Aborted` (всю операцию с перечитыванием) и `ResourceExhausted` с `RetryInfo`, и только идемпотентные вызовы или с ключом идемпотентности. `DeadlineExceeded` при том же контексте бессмысленно повторять: время уже вышло. Подробнее о повторах и backoff — `12-microservices/05`.

## Задание

Напишите перевод доменных ошибок в статусы и клиентское решение о повторе. Доменные ошибки уже объявлены в шаблоне:

```go
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrConflict      = errors.New("conflict")
)

const Domain = "orders.example.com"

type FieldViolation struct{ Field, Description string }
type ValidationError struct{ Violations []FieldViolation }
type PreconditionError struct{ Type, Subject, Description string }
type UnavailableError struct{ RetryAfter time.Duration }

func ToStatus(err error) *status.Status
func ShouldRetry(err error) (bool, time.Duration)
```

**`ToStatus(err)`**: `nil` → `nil`. Иначе применяется **первое** подходящее правило по порядку (ошибки могут быть обёрнуты `%w` или собраны `errors.Join`: переменные сравниваются через `errors.Is`, типы — через `errors.As`):

1. `context.Canceled` → `Canceled`, сообщение `canceled`, без деталей.
2. `context.DeadlineExceeded` → `DeadlineExceeded`, `deadline exceeded`, без деталей.
3. `*ValidationError` → `InvalidArgument`, `invalid request`, одна деталь `errdetails.BadRequest` с нарушениями `Field`/`Description` в том же порядке (для пустого списка — пустой `BadRequest`).
4. `*PreconditionError` → `FailedPrecondition`, `precondition failed`, одна деталь `errdetails.PreconditionFailure` с одним нарушением `Type`, `Subject`, `Description`.
5. `ErrNotFound` → `NotFound`, `not found`; `ErrAlreadyExists` → `AlreadyExists`, `already exists`; `ErrConflict` → `Aborted`, `conflict`. У каждого одна деталь `errdetails.ErrorInfo` с `Reason` `NOT_FOUND`, `ALREADY_EXISTS`, `CONFLICT` соответственно, `Domain` = `Domain` и без метаданных. (Это три правила в указанном порядке.)
6. `*UnavailableError` → `Unavailable`, `temporarily unavailable`; если `RetryAfter > 0` — одна деталь `errdetails.RetryInfo` с этой задержкой, иначе без деталей.
7. Всё остальное, включая ошибки, в которых уже есть статус gRPC, → `Internal`, `internal error`, без деталей; текст исходной ошибки в статус не попадает.

**`ShouldRetry(err)`** — для ошибки, полученной клиентом из вызова:

- `nil` или ошибка без статуса (`status.FromError` вернул `false`) → `false, 0`.
- `RetryInfo` — первая деталь этого типа в статусе; следующие `RetryInfo`, если они есть, не учитываются. Если у первой `RetryDelay` отсутствует, невалиден (`CheckValid` возвращает ошибку) или отрицателен, считается, что `RetryInfo` нет. Нулевая задержка валидна: `RetryInfo` есть, задержка 0.
- `Unavailable` и `Aborted` → `true` и задержка из `RetryInfo` (без неё — 0).
- `ResourceExhausted` → `true` и задержка, только если `RetryInfo` есть; иначе `false, 0`.
- Любой другой код → `false, 0`.
