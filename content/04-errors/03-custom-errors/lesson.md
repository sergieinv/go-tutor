---
title: Свои типы ошибок
summary: Ошибка-структура с полями, метод Error, pointer или value receiver и ловушка nil-указателя внутри error.
difficulty: 3
minutes: 20
prerequisites:
  - 04-errors/01-errors
  - 03-types/04-interface-values
docs:
  - title: "Go FAQ: Why is my nil error value not equal to nil?"
    url: https://go.dev/doc/faq#nil_error
  - title: "Пакет errors: As"
    url: https://pkg.go.dev/errors#As
  - title: "Пример из stdlib: os.PathError"
    url: https://pkg.go.dev/io/fs#PathError
---

Sentinel сообщает только «что случилось». Если вызывающему нужны подробности — имя поля, номер строки, путь к файлу, — объявляют свой тип ошибки: структуру с полями и методом `Error() string`. Так устроены `*os.PathError`, `*json.SyntaxError`, `*strconv.NumError`.

Функция по-прежнему возвращает `error`, а кому нужны детали, достаёт их через `errors.As`. И здесь же живёт самая известная ловушка Go: `nil`-указатель на ваш тип, возвращённый как `error`, — это не `nil`.

## Как это в C#

- В C# свой тип ошибки наследует `Exception` и получает стектрейс, `InnerException`, `Data`. В Go — любая структура с методом `Error()`, без базового класса и без стектрейса.
- `catch (ValidationException e)` — это `errors.As(err, &ve)`: найти ошибку нужного типа и получить доступ к её полям.
- Иерархий исключений (`IOException` → `FileNotFoundException`) в Go нет. Для группировки используют sentinel внутри своего типа или метод `Is` — об этом в уроке про wrapping.
- `null`-ссылка в C#, приведённая к базовому типу, остаётся `null`. В Go интерфейс хранит пару «тип + значение», и `nil`-указатель с типом даёт не-`nil` интерфейс.

## Под капотом

Значение интерфейса — пара слов: указатель на описание типа и указатель на данные. `err == nil` истинно, только когда пусты оба. Строка `var p *ValidationError; var err error = p` записывает в первое слово тип `*ValidationError`, а во второе — `nil`. Интерфейс уже «знает» тип, поэтому `err != nil`, и вызов `err.Error()` дойдёт до метода с `nil`-получателем.

Pointer или value receiver у метода `Error()`? Обычно — указатель: ошибку не копируют при передаче, а `errors.As` ищет ровно `*T`. С value receiver ошибкой становятся и `T`, и `*T`, а значения `T` сравниваются через `==` по полям — две разные ошибки с одинаковыми полями окажутся равны, и `errors.Is` их не различит. Главное — выбрать что-то одно и возвращать всегда один и тот же вид: `errors.As(err, &target)` ищет точный тип `target`, и `*T` не найдётся по цели типа `T`.

## Ловушки

### `nil`-указатель, возвращённый как `error`

```go
func validate(s string) error {
	var err *ValidationError // nil-указатель
	if s == "" {
		err = &ValidationError{Field: "s"}
	}
	return err // всегда не-nil интерфейс!
}

fmt.Println(validate("ok") != nil) // true
```

Возвращайте `nil` явно: `return nil`. И не объявляйте результат функции конкретным типом ошибки (`func f() *ValidationError`) — при передаче дальше как `error` случится то же самое.

### `errors.As` с неверной целью

```go
var ve ValidationError       // значение, а метод Error — на указателе
errors.As(err, &ve)          // panic: *target must be interface or implement error
var vp *ValidationError
errors.As(err, &vp)          // так правильно: цель — указатель на *ValidationError
```

Второй аргумент `errors.As` — указатель на переменную того типа, который реализует `error`.

### Value receiver и сравнение

```go
type CodeError struct{ Code int }
func (e CodeError) Error() string { return fmt.Sprint("code ", e.Code) }

fmt.Println(CodeError{404} == CodeError{404}) // true
fmt.Println(errors.Is(fmt.Errorf("x: %w", CodeError{404}), CodeError{404})) // true
```

С value receiver ошибка ведёт себя как sentinel по значению полей. Иногда это и нужно, но чаще — неожиданность. Если сомневаетесь, используйте pointer receiver.

## Задание

Напишите функцию `Check(s string) error`, которая проверяет расстановку скобок `()`, `[]`, `{}` в строке. Остальные символы игнорируются.

Тип ошибки `BracketError` уже объявлен в шаблоне:

```go
type BracketError struct {
	Pos  int  // байтовый индекс ошибочного символа или len(s), если строка кончилась
	Char rune // ошибочный символ; 0, если строка кончилась
}
```

Требования:

- скобки расставлены верно — вернуть `nil`, причём настоящий `nil`-интерфейс;
- закрывающая скобка без пары или не того вида — `&BracketError{Pos: i, Char: c}`, где `i` — её байтовый индекс;
- строка кончилась, а скобки не закрыты — `&BracketError{Pos: len(s), Char: 0}`;
- возвращается первая найденная ошибка;
- метод `(*BracketError).Error()` возвращает `unexpected ']' at 3` (символ в формате `%q`) или `unexpected end at 5`, если `Char == 0`.
