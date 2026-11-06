---
title: Sentinel-ошибки
summary: Заранее объявленные значения ошибок вроде io.EOF, сравнение через errors.Is и почему нельзя сравнивать текст.
difficulty: 2
minutes: 15
prerequisites:
  - 04-errors/01-errors
docs:
  - title: "Go Blog: Working with Errors in Go 1.13"
    url: https://go.dev/blog/go1.13-errors
  - title: "Пакет errors: Is"
    url: https://pkg.go.dev/errors#Is
  - title: "Пакет io: EOF"
    url: https://pkg.go.dev/io#EOF
---

Sentinel-ошибка («сигнальная») — значение ошибки, объявленное заранее как переменная пакета: `io.EOF`, `sql.ErrNoRows`, `os.ErrNotExist`. Вызывающий код сравнивает полученную ошибку с этим значением и по результату решает, что делать: `io.EOF` — нормальный конец данных, `sql.ErrNoRows` — «не нашли», а не авария.

Сравнивают через `errors.Is(err, io.EOF)`. Он проверяет не только саму ошибку, но и всю цепочку обёрток — поэтому sentinel остаётся узнаваемым, даже когда к нему добавили контекст.

## Как это в C#

- В C# вид сбоя различают по типу исключения: `catch (FileNotFoundException)`. Sentinel в Go — это не тип, а конкретное значение: все `ErrNotFound` — один и тот же объект.
- `errors.Is(err, ErrNotFound)` решает задачу `catch (NotFoundException)` или фильтра `catch ... when`, но проверяет всю цепочку обёрток: «это та самая ошибка, возможно, обёрнутая». `catch` по типу смотрит только на внешнее исключение, `InnerException` он не разворачивает.
- Конец потока в C# — `Read` вернул 0 или `ReadLine` вернул `null`. В Go конец данных — ошибка `io.EOF`, но ожидаемая: её проверяют и выходят из цикла.
- `sql.ErrNoRows` — аналог `FirstOrDefault`, вернувшего `null`, только явный: «не нашли» нельзя спутать с пустой строкой в базе.

## Ловушки

### Сравнение текста ошибки

```go
if err.Error() == "not found" { // сломается, как только кто-то поправит текст
	...
}
if strings.Contains(err.Error(), "not found") { // или добавит контекст
	...
}
```

Текст ошибки — для людей и логов, а не для логики. Сравнивайте значения: `errors.Is(err, ErrNotFound)`.

### Сравнение через `==` вместо `errors.Is`

```go
err := fmt.Errorf("user 42: %w", ErrNotFound)
fmt.Println(err == ErrNotFound)          // false — это уже другая ошибка
fmt.Println(errors.Is(err, ErrNotFound)) // true — Is смотрит в цепочку
```

`==` работает, только пока ошибку никто не обернул. Пишите `errors.Is` всегда — это бесплатно и не сломается после рефакторинга. Подробно об обёртках — в уроке про wrapping.

### Два `errors.New` с одинаковым текстом — разные ошибки

```go
fmt.Println(errors.New("eof") == errors.New("eof")) // false
```

`errors.New` каждый раз создаёт новое значение. Поэтому sentinel объявляют один раз переменной пакета и всегда возвращают именно её, а не создают «такую же» на месте.

## Задание

Напишите функцию `Withdraw(balances map[string]int, account string, amount int) error`, которая списывает `amount` со счёта `account`.

Требования:

- `amount <= 0` — вернуть `ErrInvalidAmount`;
- счёта нет в `balances` (в том числе `balances == nil`) — вернуть `ErrNoAccount`;
- на счёте меньше `amount` — вернуть `ErrInsufficientFunds`;
- проверки идут в этом порядке: сначала сумма, потом счёт, потом баланс;
- при любой ошибке баланс не меняется, при успехе — уменьшается на `amount` и возвращается `nil`;
- три sentinel-ошибки объявлены в шаблоне — возвращайте именно их (можно с контекстом через `%w`), тесты проверяют их через `errors.Is`.
