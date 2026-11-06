---
title: Операции со срезами
summary: copy, пакет slices, удаление и вставка, полное выражение среза s[a:b:c] и многомерные срезы.
difficulty: 2
minutes: 20
prerequisites:
  - 02-basics/05-slices
docs:
  - title: "Пакет slices"
    url: https://pkg.go.dev/slices
  - title: "Спецификация: Appending and copying slices"
    url: https://go.dev/ref/spec#Appending_and_copying_slices
  - title: "Спецификация: Slice expressions"
    url: https://go.dev/ref/spec#Slice_expressions
---

Методов у среза нет — всё, что в C# делают методы `List<T>` и LINQ, в Go делают встроенные функции `append` и `copy` и обобщённые функции пакета `slices`: `Contains`, `Index`, `Sort`, `Insert`, `Delete`, `Clone`, `Equal` и другие.

Почти все они работают с массивом под срезом напрямую. `slices.Sort` сортирует на месте, а `Insert` и `Delete` сдвигают элементы в том же массиве, если хватает ёмкости, и возвращают новый заголовок. Поэтому правило из прошлого урока действует и здесь: результат присваивают, а старые срезы на тот же массив после операции считают испорченными.

## Как это в C#

- `list.Contains(x)`, `list.IndexOf(x)`, `list.Sort()` → `slices.Contains(s, x)`, `slices.Index(s, x)`, `slices.Sort(s)`. Функции, а не методы: у среза нет методов.
- `list.RemoveAt(i)` и `list.Insert(i, x)` меняют список на месте. `slices.Delete` и `slices.Insert` возвращают новый заголовок — его нужно присвоить: `s = slices.Delete(s, i, i+1)`.
- `list.ToList()` или `new List<T>(list)` — это `slices.Clone(s)`. А `Array.Copy` — `copy(dst, src)`, но копирует он `min(len(dst), len(src))` элементов и ничего не выделяет.
- `a.SequenceEqual(b)` → `slices.Equal(a, b)`. `nil`-срез и пустой срез при этом равны.

## Ловушки

### `slices.Delete` портит исходный срез

```go
s := []int{1, 2, 3, 4, 5}
t := slices.Delete(s, 1, 3)
fmt.Println(t) // [1 4 5]
fmt.Println(s) // [1 4 5 0 0] — тот же массив, хвост обнулён
```

`Delete` сдвигает элементы в общем массиве и (с Go 1.22) обнуляет освободившийся хвост. Старый `s` после этого использовать нельзя. Если исходник нужен, удаляйте из копии: `slices.Delete(slices.Clone(s), 1, 3)`. С `slices.Insert` то же самое: при свободной ёмкости вставка сдвигает элементы в общем массиве.

### `copy` копирует `min(len(dst), len(src))`

```go
src := []int{1, 2, 3}
dst := make([]int, 0, len(src)) // len 0!
n := copy(dst, src)
fmt.Println(n, dst) // 0 []
```

`copy` не растит срез и не смотрит на ёмкость — только на длины. Для копии нужен `make([]int, len(src))` или просто `slices.Clone(src)`.

### Удаление через `append` тоже пишет в общий массив

```go
s := []int{1, 2, 3, 4}
t := append(s[:1], s[2:]...)
fmt.Println(s, t) // [1 3 4 4] [1 3 4]
```

Это тот же сдвиг на месте, что и в `slices.Delete`, только без обнуления хвоста.

## Задание

Напишите две функции. Обе **не должны менять** исходный срез, и их результат не должен делить с ним память: изменение результата не отражается на `s`.

- `Without(s []int, i int) []int` возвращает новый срез без элемента с индексом `i`, порядок остальных сохраняется. Если `i` вне диапазона `[0, len(s))`, возвращается копия `s`. Для пустого или `nil` `s` (тогда любой `i` вне диапазона) — пустой срез длины 0 (не `nil`).
- `TopN(s []int, n int) []int` возвращает `n` наибольших элементов `s` по убыванию (повторы сохраняются). Если `n > len(s)` — все элементы по убыванию. Если `n <= 0` — пустой срез длины 0 (не `nil`). Для пустого или `nil` `s` — тоже пустой срез длины 0 (не `nil`).
