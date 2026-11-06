---
title: Методы
summary: Методы на любом именованном типе, value и pointer receiver, method set и функции NewX вместо конструкторов.
difficulty: 2
minutes: 20
prerequisites:
  - 03-types/01-structs
  - 02-basics/11-pointers
docs:
  - title: "Спецификация: Method declarations"
    url: https://go.dev/ref/spec#Method_declarations
  - title: "Спецификация: Method sets"
    url: https://go.dev/ref/spec#Method_sets
  - title: "Go FAQ: Should I define methods on values or pointers?"
    url: https://go.dev/doc/faq#methods_on_values_or_pointers
---

Метод в Go — это функция с особым первым параметром, получателем (receiver): `func (c *Counter) Inc()`. Методы объявляются отдельно от типа, в том же пакете, и их можно повесить на любой именованный тип — не только на структуру, но и на `type Celsius float64` или `type IDs []int`.

Главный выбор — тип получателя. Value receiver `(c Counter)` получает копию, pointer receiver `(c *Counter)` — адрес и может менять исходное значение. От этого выбора зависит и то, какие интерфейсы реализует тип (об этом — в уроке про интерфейсы).

## Как это в C#

- В C# методы живут внутри класса, `this` неявный. В Go получатель — явный параметр с обычным именем (`c`, а не `this`/`self`), а методы можно разнести по файлам пакета.
- Метод нельзя добавить к чужому типу, даже к `int`, — нужен свой тип `type MyInt int`. Ближайший аналог extension-методов C# — обычная функция пакета.
- Конструкторов нет. Пишут функцию `NewCounter(...) *Counter`, а там, где можно, делают полезным нулевое значение: `var c Counter` сразу готов к работе.
- Нет перегрузки: у типа не может быть двух методов с одним именем, как и у пакета — двух функций.

## Ловушки

### Value receiver меняет копию

```go
func (c Counter) Inc() { c.n++ } // увеличивает копию
c.Inc()
fmt.Println(c.n) // 0
```

Если метод меняет состояние — получатель должен быть указателем. Правило: если хотя бы одному методу нужен `*T`, сделайте указателем всех, чтобы не смешивать семантику.

### `T` не реализует интерфейс с методом на `*T`

```go
type Incer interface{ Inc() }
func (c *Counter) Inc() { c.n++ }

var i Incer = Counter{}  // ошибка: Counter does not implement Incer (method Inc has pointer receiver)
var j Incer = &Counter{} // так можно
```

Вызов `c.Inc()` на переменной компилятор превращает в `(&c).Inc()`. Но значение внутри интерфейса не адресуемо, поэтому в method set типа `T` методы на `*T` не входят.

### Method value захватывает копию получателя

```go
c := Counter{}
show := c.Show // Show — метод с value receiver
c.n = 5
show() // напечатает 0: получатель скопирован в момент c.Show
```

## Задание

Реализуйте стек целых чисел `Stack` на срезе.

Требования:

- нулевое значение `var s Stack` — пустой стек, готовый к работе;
- `NewStack(items ...int) *Stack` — стек, где последний аргумент — вершина; стек не должен зависеть от среза, переданного вызывающим (изменение исходного среза не меняет стек);
- `Push(v int)` кладёт значение на вершину;
- `Pop() (int, bool)` снимает вершину; на пустом стеке возвращает `0, false`;
- `Peek() (int, bool)` возвращает вершину без снятия; на пустом стеке — `0, false`;
- `Len() int` — количество элементов.
