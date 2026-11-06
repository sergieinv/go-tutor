---
title: Generics
summary: Функции и типы с параметрами типов, вывод типов, any и comparable — и чем generics Go слабее generics C#.
difficulty: 3
minutes: 25
prerequisites:
  - 03-types/03-interfaces
  - 02-basics/12-functions
docs:
  - title: "Tutorial: Getting started with generics"
    url: https://go.dev/doc/tutorial/generics
  - title: "Спецификация: Type parameter declarations"
    url: https://go.dev/ref/spec#Type_parameter_declarations
  - title: "Go Blog: An Introduction To Generics"
    url: https://go.dev/blog/intro-generics
---

Параметры типов появились в Go 1.18. Синтаксис — квадратные скобки: `func Map[T, U any](s []T, f func(T) U) []U`, `type Stack[T any] struct{ items []T }`. У каждого параметра есть ограничение (constraint) — интерфейс, которому должен удовлетворять аргумент типа. `any` разрешает любой тип, `comparable` — типы, которые можно сравнивать через `==` (нужно, например, для ключей map).

При вызове функции аргументы типа обычно выводятся из аргументов: `Map(nums, strconv.Itoa)` вместо `Map[int, string](nums, strconv.Itoa)`. Для обобщённых типов при создании значения их указывают явно: `Stack[int]{}`.

Generics в Go намеренно проще, чем в C#. Они закрывают главное — типобезопасные контейнеры и алгоритмы над срезами и map (пакеты `slices`, `maps`), — но многого из C# здесь нет.

## Как это в C#

- `List<T>` → `[T any]`; `where T : IEquatable<T>` → `[T comparable]`; `where T : IShape` → `[T Shape]`. Ограничение — всегда интерфейс.
- В C# метод класса может иметь свои параметры: `list.Select<TResult>(...)`. В Go 1.25 метод использует только параметры своего типа, а новые объявить не может, — поэтому `Map` пишут функцией пакета: `slices`-стиль вместо LINQ-цепочек. Generic-методы разрешили только в Go 1.27, и такие методы всё равно не могут реализовывать методы интерфейсов.
- Нет вариантности (`IEnumerable<out T>`): `Stack[Dog]` и `Stack[Animal]` — несвязанные типы, `[]Dog` не передать как `[]Animal`.
- Нет специализации под конкретный `T`: в .NET JIT генерирует отдельный код для каждого value-типа и вырезает ветки вроде `typeof(T) == typeof(int)`. Нет `default(T)` — его роль играет `var zero T`, нет ограничения `new()` и static abstract-членов, которые можно вызвать у `T`.

## Ловушки

### `==` требует `comparable`, а не `any`

```go
func Index[T any](s []T, v T) int {
	for i := range s {
		if s[i] == v { // ошибка: incomparable types in type set
```

`any` разрешает любой тип, включая срезы и функции, которые сравнивать нельзя. Укажите `[T comparable]`.

### Тип нельзя вывести из результата

```go
func Zero[T any]() T { var z T; return z }
x := Zero()      // ошибка: cannot infer T
x := Zero[int]() // так можно
```

Аргументы типа выводятся только из аргументов функции, а не из того, куда присваивается результат.

### Generics «на всякий случай»

```go
func Print[T fmt.Stringer](v T) { fmt.Println(v.String()) }
func Print(v fmt.Stringer)      { fmt.Println(v.String()) } // проще и делает то же
```

Если параметр типа используется только для вызова методов, обычный интерфейс проще. Generics нужны, когда тип связывает несколько мест: вход и выход, элементы контейнера, ключи map.

## Задание

Напишите обобщённые функции и тип:

- `Uniq[T comparable](s []T) []T` — элементы без повторов в порядке первого появления; для пустого или `nil`-входа — пустой срез длины 0 (не `nil`); вход не меняется;
- `GroupBy[T any, K comparable](s []T, key func(T) K) map[K][]T` — группирует элементы по ключу, порядок внутри группы — как во входе; для пустого входа — пустая (не `nil`) map;
- `Set[T comparable]` — множество с методами `Add(v T)`, `Has(v T) bool` и `Len() int`. Нулевое значение `var s Set[string]` должно быть готово к работе: `Add` на нём не паникует.
