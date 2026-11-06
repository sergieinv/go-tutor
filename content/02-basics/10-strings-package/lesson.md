---
title: Пакет strings и strconv
summary: Функции strings вместо методов string, strings.Builder вместо += в цикле, strconv для чисел.
difficulty: 2
minutes: 20
prerequisites:
  - 02-basics/09-strings-and-runes
docs:
  - title: "Пакет strings"
    url: https://pkg.go.dev/strings
  - title: "Пакет strconv"
    url: https://pkg.go.dev/strconv
---

У строки в Go нет методов: вместо `s.Split(",")` пишут `strings.Split(s, ",")`. Пакет `strings` закрывает почти всё, что в C# дают методы `System.String`: поиск, разбиение, замену, обрезку пробелов, сравнение без учёта регистра. Для сборки строки по частям есть `strings.Builder` — аналог `StringBuilder`.

Числа в строки и обратно переводит пакет `strconv`. Здесь нет `int.Parse` с исключением: `strconv.Atoi` возвращает число и ошибку, и проверить её — ваша обязанность.

## Как это в C#

- `s.Contains(x)`, `s.Split(',')`, `string.Join` → `strings.Contains(s, x)`, `strings.Split(s, ",")`, `strings.Join(parts, ",")`. Функции пакета, а не методы.
- `s.Trim()` → `strings.TrimSpace(s)`. Осторожно: `strings.Trim(s, "ab")` убирает **набор символов** с краёв, а не подстроку.
- `int.Parse` бросает `FormatException`, `int.TryParse` возвращает `bool`. `strconv.Atoi` возвращает `(int, error)` — нечто среднее: без исключений, но с описанием ошибки.
- `StringBuilder` → `strings.Builder`. Нулевое значение готово к работе, `new` не нужен.

## Ловушки

### `+=` в цикле

```go
var s string
for _, w := range words {
	s += w + " " // на каждой итерации — новая строка и копия всего накопленного
}
```

Строки неизменяемы, поэтому каждая конкатенация копирует весь результат: на n словах это O(n²). `strings.Builder` дописывает в растущий буфер, а `strings.Join` сразу считает итоговый размер.

### `Split` и пустые поля

```go
fmt.Println(len(strings.Split("", ",")))      // 1 — срез из одной пустой строки
fmt.Println(len(strings.Split("a,,b", ","))) // 3 — пустое поле в середине
fmt.Println(len(strings.Fields("  a  b ")))  // 2 — Fields пустых полей не даёт
```

### `Atoi` не прощает пробелов и `+`

```go
_, err := strconv.Atoi(" 42")
fmt.Println(err) // strconv.Atoi: parsing " 42": invalid syntax
```

Пробелы убирайте `strings.TrimSpace` до разбора. Знак `+` `Atoi` принимает, а вот разделители тысяч и `,` вместо `.` у `ParseFloat` — нет.

## Задание

Напишите функцию `ParseKV(s string) (map[string]int, error)`, которая разбирает строку вида `"a=1; b = 2;c=3"`.

Требования:

- пары разделены `;`, ключ и значение — первым `=` в паре;
- пробелы вокруг пар, ключей и значений игнорируются;
- пустые пары, в том числе из одних пробелов (после последней `;`, в пустой строке, `"a=1;  ;b=2"`), пропускаются; для пустой строки возвращается пустая (не `nil`) map;
- ключи приводятся к нижнему регистру: `"A=1"` и `"a=1"` — один ключ, побеждает последнее значение;
- если в паре нет `=`, ключ пустой или значение не целое число — вернуть `nil` и ошибку. Целое число — то, что принимает `strconv.Atoi`: знак `+` или `-` допустим (`"a=+5"` → 5).
