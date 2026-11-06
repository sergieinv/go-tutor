---
title: Итераторы
summary: range по функциям — iter.Seq и iter.Seq2, yield и ранний выход, итераторы из slices и maps, pull-итераторы.
difficulty: 3
minutes: 25
prerequisites:
  - 03-types/07-generics
  - 02-basics/08-range
  - 02-basics/15-closures
docs:
  - title: "Go Blog: Range Over Function Types"
    url: https://go.dev/blog/range-functions
  - title: "Пакет iter"
    url: https://pkg.go.dev/iter
  - title: "Спецификация: For statements with range clause"
    url: https://go.dev/ref/spec#For_range
---

С Go 1.23 `for range` умеет обходить функции. Итератор — это функция, которая принимает callback `yield` и вызывает его для каждого элемента: `func(yield func(T) bool)`. Пакет `iter` даёт этим типам имена: `iter.Seq[T]` для последовательности значений и `iter.Seq2[K, V]` для пар.

Цикл `for v := range seq` компилятор превращает в вызов `seq` с телом цикла в роли `yield`. Когда тело делает `break` или `return`, `yield` возвращает `false` — и итератор обязан сразу прекратить работу. Стандартная библиотека отдаёт итераторы повсюду: `slices.Values`, `slices.All`, `maps.Keys`, `maps.All`, `strings.SplitSeq`, а `slices.Collect` и `slices.Sorted` собирают их обратно в срез.

## Как это в C#

- `IEnumerable<T>` с `yield return` — pull-модель: потребитель вызывает `MoveNext()`. Итератор Go — push-модель: итератор сам вызывает тело цикла через `yield`. Компилятор не строит конечный автомат, это обычный вызов функции с замыканием.
- `yield break` → `return` из итератора. Остановка со стороны потребителя (`break` в `foreach`) в Go видна итератору как `yield(...) == false`.
- `IEnumerator<T>` и ручной `MoveNext` → `iter.Pull(seq)`: возвращает функции `next()` и `stop()`. Нужен, когда надо идти по двум последовательностям одновременно (как `Zip`). `stop` обязателен — аналог `Dispose`.
- LINQ (`Where`, `Select`, `Take`) для итераторов в стандартной библиотеке нет — такие адаптеры пишут сами, это несколько строк.

## Ловушки

### Игнорировать результат `yield` нельзя

```go
func Count(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := range n {
			yield(i) // результат не проверяется
		}
	}
}
for v := range Count(5) { if v == 1 { break } }
// panic: runtime error: range function continued iteration after function for loop body returned false
```

Правильно: `if !yield(i) { return }`.

### Однократные и многократные итераторы

`slices.Values(s)` можно обходить сколько угодно раз. Итератор поверх потока (чтение файла, канал) обычно отдаёт данные один раз — второй цикл по нему окажется пустым. Документируйте, какой у вас итератор.

### Порядок `maps.Keys` случаен

```go
for k := range maps.Keys(m) { ... } // порядок меняется от запуска к запуску
keys := slices.Sorted(maps.Keys(m)) // детерминированный порядок
```

## Задание

Напишите адаптеры для `iter.Seq[T]`:

- `Filter[T any](seq iter.Seq[T], keep func(T) bool) iter.Seq[T]` — только элементы, для которых `keep` вернул `true`;
- `Take[T any](seq iter.Seq[T], n int) iter.Seq[T]` — первые `n` элементов. Итератор не должен запрашивать у `seq` больше `n` элементов; при `n <= 0` — пустая последовательность, `seq` вообще не вызывается;
- `Fib() iter.Seq[int]` — бесконечная последовательность Фибоначчи `0, 1, 1, 2, 3, 5, …`.

Все итераторы обязаны корректно останавливаться, когда потребитель прекращает цикл, и их можно обходить повторно.
