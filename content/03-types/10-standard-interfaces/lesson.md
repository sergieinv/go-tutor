---
title: Интерфейсы стандартной библиотеки
summary: fmt.Stringer, error, io.Reader и io.Writer и их композиция через io.Copy, sort.Interface против slices.SortFunc.
difficulty: 3
minutes: 25
prerequisites:
  - 03-types/03-interfaces
  - 03-types/06-embedding
docs:
  - title: "Пакет io"
    url: https://pkg.go.dev/io
  - title: "Пакет fmt: Stringer"
    url: https://pkg.go.dev/fmt#Stringer
  - title: "Пакет slices: SortFunc"
    url: https://pkg.go.dev/slices#SortFunc
---

Несколько маленьких интерфейсов стандартной библиотеки связывают между собой почти весь Go-код. Реализуете один метод — и ваш тип начинает работать с десятками чужих функций.

- `fmt.Stringer` — `String() string`: как значение печатается в `fmt`.
- `error` — `Error() string`: встроенный интерфейс ошибки (подробно — в модуле 4).
- `io.Reader` — `Read(p []byte) (n int, err error)`: «заполни мой буфер». Источник сообщает о конце данных ошибкой `io.EOF`.
- `io.Writer` — `Write(p []byte) (n int, err error)`: «запиши эти байты».

Файлы, сетевые соединения, HTTP-тела, `strings.Reader`, `bytes.Buffer`, gzip, хэши — всё это `Reader` и/или `Writer`. Поэтому `io.Copy(dst, src)` копирует из любого источника в любой приёмник, а обёртки (сжатие, подсчёт, шифрование) собираются в цепочки, как матрёшки.

## Как это в C#

- `ToString()` есть у каждого объекта. `String()` в Go — обычный метод, и `fmt` вызывает его, только если тип реализует `fmt.Stringer`.
- `Stream` — один большой абстрактный класс с `Read`, `Write`, `Seek`, `CanRead`… В Go это отдельные интерфейсы `io.Reader`, `io.Writer`, `io.Seeker`, а комбинации собираются встраиванием: `io.ReadWriter`, `io.ReadCloser`.
- `stream.CopyTo(other)` → `io.Copy(dst, src)`; `MemoryStream` → `bytes.Buffer`; `StringReader` → `strings.NewReader`.
- `IComparer<T>` и `List<T>.Sort(comparison)` → `slices.SortFunc(s, cmp)`. Старый `sort.Interface` (`Len`, `Less`, `Swap`) похож на реализацию `IComparer` целым типом — в новом коде его заменяют `slices.SortFunc`.

## Ловушки

### `Read` может вернуть данные вместе с ошибкой

```go
n, err := r.Read(buf)
if err != nil {
	return err // потеряны n байт, прочитанных вместе с io.EOF
}
```

Контракт `io.Reader` разрешает вернуть `n > 0` и `err != nil` одновременно. Сначала обработайте `buf[:n]`, потом смотрите на ошибку. А лучше не писать цикл чтения вручную: есть `io.ReadAll`, `io.Copy`, `bufio.Scanner`.

### `String()` на указателе не работает для значения

```go
func (t *Temp) String() string { return "..." }
fmt.Println(Temp{20})  // {20} — у значения Temp нет метода String
fmt.Println(&Temp{20}) // ...
```

### Рекурсия в `String()`

```go
func (u User) String() string { return fmt.Sprintf("%v", u) } // бесконечная рекурсия
```

`%v` снова вызовет `String()`. Форматируйте поля по отдельности или приведите к типу без методов: `type plain User; fmt.Sprintf("%v", plain(u))`.

## Задание

Реализуйте обёртку-писатель и обёртку-читатель.

`CountingWriter{ W io.Writer; Bytes int64; Lines int }` с методом `Write` на указателе:

- передаёт `p` во вложенный `W` и возвращает его `n` и `err` без изменений;
- увеличивает `Bytes` на `n` — сколько байт реально записал `W`, а не `len(p)`; так же и при ошибке от `W`;
- увеличивает `Lines` на количество символов `'\n'` среди записанных байт `p[:n]`.

`UpperReader{ R io.Reader }` с методом `Read`:

- читает из `R` и переводит в верхний регистр латинские буквы `a`–`z` среди прочитанных байт; остальные байты (цифры, кириллица в UTF-8 и т.д.) не меняет;
- возвращает `n` и `err` от `R` без изменений, в том числе когда данные пришли вместе с ошибкой.
