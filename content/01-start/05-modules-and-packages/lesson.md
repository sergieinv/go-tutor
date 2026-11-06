---
title: Модули и пакеты
summary: go.mod, модуль и пакет, import, видимость по регистру первой буквы, пакет = каталог и каталог internal/.
difficulty: 2
minutes: 20
prerequisites:
  - 01-start/01-hello-world
docs:
  - title: "Tutorial: Create a Go module"
    url: https://go.dev/doc/tutorial/create-module
  - title: "Go Modules Reference"
    url: https://go.dev/ref/mod
  - title: "Спецификация: Exported identifiers"
    url: https://go.dev/ref/spec#Exported_identifiers
---

Код Go организован в два уровня. **Пакет** — это каталог: все `.go`-файлы в нём объявляют один и тот же `package` и видят друг друга целиком, без всяких `partial`. **Модуль** — дерево пакетов с файлом `go.mod` в корне: он задаёт путь модуля, версию Go и зависимости.

```sh
go mod init example.com/shop   # создаёт go.mod
go get github.com/google/uuid  # добавляет зависимость в go.mod и go.sum
go mod tidy                    # убирает лишние зависимости, дописывает нужные
```

```
shop/
  go.mod               module example.com/shop
  cmd/shop/main.go     package main      → example.com/shop/cmd/shop
  order/order.go       package order     → example.com/shop/order
  order/total.go       package order     (тот же пакет)
  internal/money/money.go  package money → example.com/shop/internal/money
```

Путь импорта пакета — путь модуля плюс каталог: `import "example.com/shop/order"`. Имя, через которое к пакету обращаются в коде, — это имя из объявления `package`, по соглашению совпадающее с последним элементом пути: `order.Total(...)`.

```go
// go.mod
module example.com/shop

go 1.25

require github.com/google/uuid v1.6.0
```

Модификаторов доступа в Go нет. Видимость решает **регистр первой буквы**: `Total`, `Order`, `MaxItems` экспортируются из пакета, `total`, `order`, `maxItems` видны только внутри него. Правило одно для функций, типов, переменных, констант, полей структур и методов. Пакеты из каталога `internal/` можно импортировать только из дерева его родителя: `example.com/shop/internal/money` доступен `example.com/shop/...`, но не другим модулям.

```go
// order/order.go
package order

import "example.com/shop/internal/money"

// Total экспортирована — её видно из cmd/shop.
func Total(prices []int64) money.Amount {
	return money.Amount(sum(prices))
}

// sum не экспортирована — она видна во всех файлах пакета order, и только в них.
func sum(xs []int64) int64 { /* ... */ }
```

## Как это в C#

- `namespace` в C# логический и не связан с файлами и сборками. Пакет Go — физический: один каталог — один пакет, и путь импорта совпадает с путём каталога.
- `public`/`private`/`internal`/`protected` заменяет регистр первой буквы, а уровней всего два: «виден снаружи пакета» и «виден внутри пакета». Ближайший аналог `internal` уровня сборки — каталог `internal/`.
- `.csproj` с `PackageReference` и `packages.lock.json` — это `go.mod` и `go.sum`. Но lock-файл NuGet фиксирует и версии, и хеши, а в Go версии фиксирует сам `go.mod`, `go.sum` хранит только контрольные суммы. Выбор версий похож на правило NuGet «наименьшая подходящая»: Go берёт минимальную версию, удовлетворяющую всем требованиям, а не последнюю, — только без диапазонов и плавающих версий.
- Циклические ссылки между проектами C# запрещены — и между пакетами Go тоже: `import cycle not allowed`. Общий код выносят в третий пакет.

## Ловушки

### Неэкспортированное поле не видно другим пакетам — и `encoding/json` тоже

```go
type User struct {
	Name string
	age  int // encoding/json — другой пакет, он поля не видит
}
b, _ := json.Marshal(User{"Ann", 30})
fmt.Println(string(b)) // {"Name":"Ann"}
```

Никакой ошибки не будет — поле просто молча пропущено. То же с рефлексией, шаблонами и любыми библиотеками сериализации.

### Префикс пути — не дерево

`internal/` проверяет дерево каталогов, а не строковый префикс: `example.com/shop2` не может импортировать `example.com/shop/internal/money`, хотя строка `example.com/shop2` начинается с `example.com/shop`.

### Один каталог — один пакет

```
order/order.go   package order
order/util.go    package utils   // found packages order (order.go) and utils (util.go)
```

Все файлы каталога должны объявлять один пакет. Исключение — тесты с суффиксом `_test` в имени пакета (`package order_test`), они проверяют пакет «снаружи».

## Задание

Напишите две функции, которые проверяют правила видимости Go.

`IsExported(name string) bool` сообщает, экспортируется ли идентификатор `name`: первый символ — заглавная буква Unicode (`unicode.IsUpper`). `"User"` и `"Ёлка"` экспортируются, `"user"`, `"_User"` и `""` — нет.

`CanImport(importer, imported string) bool` сообщает, может ли пакет с путём `importer` импортировать пакет `imported`.

- если в `imported` нет элемента `internal`, импорт разрешён;
- элемент — целый сегмент пути между `/`: `internals` или `myinternal` правило не включают;
- если `internal` есть, импорт разрешён, только когда `importer` лежит в дереве родителя `internal`: его путь равен пути родителя или начинается с пути родителя и `/`;
- если элементов `internal` несколько, решает последний (самый глубокий);
- `internal` никогда не бывает первым элементом пути — пути всегда начинаются с имени модуля вроде `example.com/app`.
