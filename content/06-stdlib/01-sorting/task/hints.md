## Подсказка 1

`slices.SortFunc` сортирует на месте — тот самый срез, который передал вызывающий. Сначала сделайте копию: `slices.Clone(players)`, и сортируйте её.

## Подсказка 2

Функция сравнения возвращает `int`. Сравнение по одному ключу — `cmp.Compare(a.Time, b.Time)`; по убыванию — поменяйте аргументы местами: `cmp.Compare(b.Score, a.Score)`. Не вычитайте: `a.Score - b.Score` переполняется на больших значениях.

## Подсказка 3

```go
slices.SortFunc(res, func(a, b Player) int {
	return cmp.Or(
		cmp.Compare(b.Score, a.Score),
		cmp.Compare(a.Time, b.Time),
		cmp.Compare(a.Name, b.Name),
	)
})
```
