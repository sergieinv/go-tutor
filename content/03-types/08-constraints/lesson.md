---
title: Ограничения типов
summary: Интерфейсы-ограничения, объединения типов, ~T, пакет cmp — и когда generics не нужны.
difficulty: 3
minutes: 20
prerequisites:
  - 03-types/07-generics
docs:
  - title: "Спецификация: General interfaces"
    url: https://go.dev/ref/spec#General_interfaces
  - title: "Пакет cmp"
    url: https://pkg.go.dev/cmp
  - title: "Go Blog: When To Use Generics"
    url: https://go.dev/blog/when-generics
---

`any` и `comparable` разрешают немногое: присваивать и сравнивать. Чтобы складывать, умножать или сравнивать через `<`, ограничение должно перечислить допустимые типы. Для этого в интерфейсах-ограничениях есть объединения: `interface{ int | int64 | float64 }` — «один из этих типов». Операция разрешена в теле функции, если она есть у всех типов из набора.

Знак `~` расширяет набор до всех типов с данным базовым типом: `~int` — это `int`, `type Celsius int`, `type UserID int` и любые другие типы, объявленные «поверх» `int`. Без `~` пользовательские типы не подходят — а в Go их много, от `time.Duration` до ваших enum.

Готовые ограничения лежат в стандартной библиотеке: `cmp.Ordered` — все типы, поддерживающие `<` (целые, вещественные, строки, с `~`). Там же `cmp.Compare` и `cmp.Less` — для сортировки через `slices.SortFunc`.

## Как это в C#

- В C# 11 арифметику в generics дают `INumber<T>` и static abstract-члены интерфейсов. В Go вместо этого перечисляют типы: `~int | ~float64`, а операторы `+` и `<` работают напрямую.
- `where T : IComparable<T>` и `CompareTo` → `cmp.Ordered` и обычный `<`; `Comparer<T>.Default.Compare` → `cmp.Compare`.
- Интерфейс с объединением — только ограничение. Переменную типа `interface{ int | float64 }` объявить нельзя. В C# (включая версию 14) union-типов нет вовсе.
- `IComparable<T>` реализует любой класс. `cmp.Ordered` — только встроенные упорядоченные типы и типы на их основе; для своих структур передают функцию сравнения (`slices.SortFunc`, `slices.MaxFunc`).

## Ловушки

### Без `~` свой тип не подходит

```go
type Number interface{ int | float64 }
type Cents int
Sum([]Cents{1, 2}) // ошибка: Cents does not satisfy Number (possibly missing ~ for int in Number)
```

Если нет причин запрещать именованные типы, пишите `~int | ~float64`.

### Ограничение — не тип значения

```go
var x Number // ошибка: cannot use type Number outside a type constraint: interface contains type constraints
```

Интерфейсы с объединениями или `~` существуют только в квадратных скобках. Для «одного из нескольких типов» в обычном коде используют `any` и type switch.

### Generics там, где хватит интерфейса

Если функция только вызывает методы параметра, параметр типа ничего не даёт — достаточно интерфейса. Generics полезны, когда нужны операторы (`+`, `<`), когда тип связывает вход и выход или когда важно не упаковывать значения в интерфейс в горячем цикле.

## Задание

Дано ограничение:

```go
type Number interface {
	~int | ~int64 | ~float64
}
```

Напишите:

- `Sum[T Number](s []T) T` — сумма элементов; для пустого среза — 0. Должна работать и для пользовательских типов вроде `type Cents int64`;
- `Clamp[T cmp.Ordered](v, lo, hi T) T` — `v`, ограниченное отрезком `[lo, hi]`; если `lo > hi` — паника;
- `MaxBy[T any, K cmp.Ordered](s []T, key func(T) K) (T, bool)` — элемент с наибольшим ключом; при равных ключах — первый из них; для пустого среза — нулевое значение и `false`.
