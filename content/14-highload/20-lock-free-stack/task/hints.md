## Подсказка 1

Вершина — `atomic.Pointer[node[T]]`, где `node` хранит значение и указатель на следующий узел. Нулевой `atomic.Pointer` равен `nil` — это и есть пустой стек, конструктор не нужен. `Push` и `Pop` — CAS-циклы из примера урока, только обобщённые.

## Подсказка 2

`PushMany`: соберите цепочку узлов заранее — первый аргумент внизу, последний наверху, — пока её никто не видит. Потом в CAS-цикле привязывайте нижний узел цепочки к текущей вершине и меняйте вершину на верхний узел одним `CompareAndSwap`. `Drain`: `Swap(nil)` атомарно отрывает весь стек, а дальше по цепочке можно идти обычным циклом — эти узлы уже никто не изменит.

## Подсказка 3

```go
bottom := &node[T]{v: vs[0]}
top := bottom
for _, v := range vs[1:] {
	top = &node[T]{v: v, next: top}
}
for {
	old := s.head.Load()
	bottom.next = old
	if s.head.CompareAndSwap(old, top) {
		return
	}
}
```

В `Drain`: `for n := s.head.Swap(nil); n != nil; n = n.next { out = append(out, n.v) }`.
