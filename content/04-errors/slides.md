# Ошибки

Обзор модуля для показа команде: ошибка в Go — обычное значение, а не исключение; `defer` вместо `finally`, `panic` — только для багов.

---

## Ошибка — последний результат

```go
n, err := strconv.Atoi(s)
if err != nil {
	return 0, fmt.Errorf("строка %d: %w", line, err)
}
```

| C# | Go |
|---|---|
| `throw` летит вверх сам | ошибку явно возвращают на каждом уровне |
| сигнатура молчит об исключениях | последний результат `error` — вызов может не удаться |
| стектрейс в исключении | дешёвое значение без стектрейса, контекст — текстом |

---

## Три способа потерять ошибку

- `n, _ := strconv.Atoi("12a")` — ошибка выброшена, программа тихо работает с нулём.
- Читать результат при `err != nil`: `Atoi` при переполнении возвращает максимальное `int`, а не ноль.
- Вернуть `err` без контекста: через пять уровней «invalid syntax» уже ничего не говорит.

Каждый уровень добавляет то, что знает только он: что делал и с какими аргументами.

---

## Sentinel-ошибки и `errors.Is`

`io.EOF`, `sql.ErrNoRows`, `os.ErrNotExist` — заранее объявленные значения. Вместо `catch (NotFoundException)`:

```go
var ErrNotFound = errors.New("not found")

err := fmt.Errorf("user 42: %w", ErrNotFound)
fmt.Println(err == ErrNotFound)          // false — уже обёрнута
fmt.Println(errors.Is(err, ErrNotFound)) // true — Is смотрит в цепочку
```

Текст ошибки — для людей и логов, не для `if`.

---

## Свой тип ошибки и `errors.As`

```go
type ValidationError struct{ Field string }

func (e *ValidationError) Error() string { return "invalid " + e.Field }

var ve *ValidationError
if errors.As(err, &ve) { // аналог catch (ValidationException e)
	fmt.Println(ve.Field)
}
```

Базового класса, иерархий и `InnerException` нет — любая структура с методом `Error()`.

---

## nil-указатель как `error` — не `nil`

```go
func validate(s string) error {
	var err *ValidationError // nil-указатель
	if s == "" {
		err = &ValidationError{Field: "s"}
	}
	return err // интерфейс (тип *ValidationError, nil) — всегда не nil
}
```

Возвращайте `nil` явно и не объявляйте результат конкретным типом ошибки.

---

## Обёртки: цепочка вместо `InnerException`

| C# | Go |
|---|---|
| `new X("…", inner)` | `fmt.Errorf("load %d: %w", id, err)` |
| ручной обход `InnerException` | `errors.Is` / `errors.As` по всей цепочке |
| `AggregateException` | `errors.Join(err1, err2)` |
| наследование `Exception` | свой тип с методом `Unwrap() error` |

- `%w` делает причину частью API: на границе пакета переводите в свои ошибки или пишите `%v`.
- `fmt.Errorf("…: %w", nil)` — не `nil`. Оборачивают только внутри `if err != nil`.

---

## `defer` вместо `using` и `finally`

```go
f, err := os.Open(path)
if err != nil {
	return err
}
defer f.Close() // выполнится при любом выходе из функции
```

- Отложенные вызовы выполняются в обратном порядке.
- Аргументы вычисляются **в момент `defer`**, а не при выполнении.
- `defer` привязан к функции, а не к блоку: в цикле файлы закроются только в конце функции.

---

## `panic` — для багов, а не для «файл не найден»

- `panic` раскручивает стек и выполняет отложенные вызовы каждого кадра; без `recover` — выход с кодом 2.
- `recover` работает только в функции, которую напрямую отложили: `defer func() { if r := recover(); r != nil { … } }()`.
- После `recover` функция не продолжает с места паники, а возвращает именованные результаты.
- Паника в чужой горутине этим `recover` не ловится и роняет весь процесс.

---

## Раскрутка стека по шагам

Как `panic` проходит кадры, выполняет `defer` в порядке LIFO и останавливается на `recover`.

@viz 04-errors/06-panic-recover

---

## Что запомнить

- `if err != nil` — не шум, а явный путь ошибки; `_` вместо `err` — осознанное решение с комментарием.
- Сравнивайте через `errors.Is` и `errors.As`, а не через `==` или текст.
- Возвращайте `nil` явно — nil-указатель в `error` не равен `nil`.
- `defer` — до конца функции; ошибку `Close` при записи проверяют.
- `panic` превращают в ошибку только на границе компонента.
