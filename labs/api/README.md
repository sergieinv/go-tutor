# Контракты итогового проекта

Один buf-модуль — контракты всех сервисов системы ([`labs/README.md`](../README.md)).
Контракт здесь — источник истины: из него получаются gRPC-сервер и клиент,
REST (grpc-gateway), валидация (protovalidate), OpenAPI и типы событий
Kafka. Код на любом другом языке генерируется из тех же файлов.

```
labs/api/
├── buf.yaml          модуль, зависимости (googleapis, protovalidate), lint STANDARD, breaking FILE
├── buf.lock          коммиты зависимостей (те же, что у песочницы учебника)
├── buf.gen.yaml      плагины: protoc-gen-go, -go-grpc, -grpc-gateway, -openapiv2
├── proto/
│   ├── orders/v1/orders.proto        OrderService: CreateOrder, GetOrder, ListOrders, CancelOrder
│   ├── warehouse/v1/warehouse.proto  WarehouseService: Reserve, Release, GetStock
│   └── events/v1/events.proto        OrderEvent { OrderCreated | OrderCancelled } — значение записи Kafka
└── gen/              сгенерированный код (закоммичен, руками не правится)
    ├── orders/v1/    ordersv1: *.pb.go, *_grpc.pb.go, *.pb.gw.go
    ├── warehouse/v1/ warehousev1
    ├── events/v1/    eventsv1
    └── openapi/      *.swagger.json (REST-часть)
```

Сервисы подключают модуль через `replace gotutor/labs/api => ../api`. В
компании это был бы отдельный репозиторий контрактов или Buf Schema
Registry, а сервисы брали бы версию модуля.

## Что в контрактах

- **REST — аннотации `google.api.http`.** `POST /v1/orders`,
  `GET /v1/orders/{order_id}`, `GET /v1/orders?customer_id=…&page_size=…&page_token=…`,
  `POST /v1/orders/{order_id}:cancel` (пользовательский метод, AIP-136),
  `GET /v1/stock/{product_id}`. У `Reserve`/`Release` аннотаций нет, и
  REST-маршрутов для них не существует: внутренний API наружу не торчит.
- **Валидация — правила protovalidate прямо в полях.** `int64.gt = 0`,
  `repeated.min_items/max_items`, `int32.gte/lte`, CEL-правило
  уникальности товаров `this.map(item, item.product_id).unique()`.
  Проверяет их interceptor сервера (`labs/platform/grpcx`), до обработчика.
- **Ошибки описаны в комментариях методов.** Указаны коды и детали
  (`google.rpc.ErrorInfo` с reason из `warehouse.v1.ErrorReason`,
  `PreconditionFailure`, `BadRequest`, `RetryInfo`). Клиент ветвится по
  коду и reason, а не по тексту.
- **Идемпотентность.** `Reserve`/`Release` идемпотентны по `order_id`,
  `Release` без резерва оставляет «надгробие».
- **События.** Конверт `OrderEvent` с `oneof`: события одного заказа лежат
  в одном топике и в одной партиции (ключ — `order_id`), потребитель
  разбирает их одним типом. В конверте — `event_id` для дедупликации.
  Заголовки записи описаны в `events.proto`.

## Команды

buf v1.75.0, плагины тех же версий, что в образе runner: protoc-gen-go
v1.36.12, protoc-gen-go-grpc v1.6.2, protoc-gen-grpc-gateway и
protoc-gen-openapiv2 v2.30.0.

```sh
cd labs/api
buf lint                 # стиль: STANDARD без исключений
buf format -d            # форматирование (пусто — всё в порядке)
buf generate             # перегенерировать gen/ (clean: true — старое удаляется)
go build ./...           # сгенерированный код компилируется
```

## Проверка совместимости в CI

`buf breaking` сравнивает текущие контракты с опубликованными и падает на
несовместимом изменении: смене номера или типа поля, удалении поля без
`reserved`, переименовании сообщения или метода. Набор `FILE` — самый
строгий: он ловит и то, что ломает сгенерированный код, а не только
формат на проводе.

```sh
# против основной ветки репозитория
buf breaking --against '../../.git#branch=main,subdir=labs/api'
# против последнего релиза
buf breaking --against '../../.git#tag=v1.2.0,subdir=labs/api'
```

Шаг CI (GitHub Actions):

```yaml
- uses: bufbuild/buf-action@v1
  with:
    input: labs/api
    breaking_against: "${{ github.event.repository.clone_url }}#branch=main,subdir=labs/api"
```

Плюс проверка, что `gen/` не устарел: `buf generate && git diff --exit-code labs/api/gen`.

Несовместимое изменение делается так: оставить старый пакет, рядом
завести `orders.v2`, какое-то время поддерживать оба и перевести клиентов.
