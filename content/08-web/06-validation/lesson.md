---
title: Валидация входных данных
summary: Метод Validate() error у DTO, сбор ошибок всех полей, errors.As в хендлере — 422 против 400, нормализация, длина в символах, net/mail и net/url.
difficulty: 3
minutes: 25
prerequisites:
  - 08-web/05-json-api
  - 04-errors/03-custom-errors
  - 02-basics/09-strings-and-runes
docs:
  - title: "Пакет net/mail: ParseAddress"
    url: https://pkg.go.dev/net/mail#ParseAddress
  - title: "Пакет errors: As и Join"
    url: https://pkg.go.dev/errors
  - title: "Пакет unicode/utf8"
    url: https://pkg.go.dev/unicode/utf8
---

В ASP.NET Core валидацию делает конвейер: атрибуты `[Required]`, `[StringLength]` или правила FluentValidation проверяются до вызова action, а результат лежит в `ModelState`. В Go конвейера нет, и обычный подход — без магии: у DTO есть метод `Validate() error`, хендлер вызывает его сам сразу после разбора JSON. Правила — обычный код: их видно, их легко тестировать, в них не нужна рефлексия.

Хорошая `Validate` собирает **все** ошибки полей, а не останавливается на первой, и возвращает их значением своего типа — например, `FieldErrors map[string]string`, реализующим `error`. Хендлер достаёт его через `errors.As` и отвечает `422 Unprocessable Entity` с картой «поле → сообщение». Другой вариант — тип `FieldError{Field, Msg}` на каждое поле и `errors.Join(errs...)` в конце: `Join` возвращает `nil` для пустого списка, а `errors.As` находит первое значение нужного типа внутри (все — через `Unwrap() []error`). Карта удобнее для ответа клиенту, `Join` — для композиции с другими ошибками. Синтаксически битый JSON — другая ошибка: до проверки полей дело не дошло, это `400 Bad Request`. Перед проверкой данные нормализуют: обрезают пробелы, приводят email к нижнему регистру, — иначе проверяется одно значение, а сохраняется другое.

Стандартная библиотека помогает с форматами: `net/mail.ParseAddress` разбирает адрес по RFC 5322, `net/url.Parse` — URL, `unicode/utf8.RuneCountInString` считает символы. Но каждая из этих функций принимает больше, чем обычно ожидают от поля формы, — их результат нужно дополнительно проверить (см. пример). Для больших DTO есть сторонний пакет `github.com/go-playground/validator` с тегами `` `validate:"required,email,max=50"` `` — по духу это DataAnnotations: правила в тегах, проверка через рефлексию. В песочнице его нет; руками понятнее, что именно проверяется.

## Как это в C#

- `[Required]`, `[StringLength(50, MinimumLength = 2)]`, `[EmailAddress]`, `[Url]` из DataAnnotations → строки кода в `Validate()`. Ближе всего по духу — FluentValidation: правила тоже код, но в Go отдельный класс-валидатор и регистрация в DI не нужны.
- `[ApiController]` сам возвращает `400` с `ValidationProblemDetails`, если `ModelState` невалиден, — и для битого JSON, и для неверных полей. В Go статусы выбираете вы; распространённое соглашение — `400` для синтаксиса, `422` для смысла.
- `ModelState.AddModelError(key, msg)` → запись в `FieldErrors`; `ModelState.IsValid` → `err == nil`. Ключи для вложенных полей (`items[2].qty`) тоже формируете сами.
- `string.Length` в C# считает UTF-16-единицы, `len(s)` в Go — байты UTF-8. Для «не длиннее 50 символов» в Go нужен `utf8.RuneCountInString`.

## Ловушки

### Типизированный nil вместо «ошибок нет»

```go
func (u CreateUser) Validate() error {
	var errs FieldErrors // nil-карта
	// ... ни одной ошибки ...
	return errs // интерфейс error с типом FieldErrors и значением nil
}
err := u.Validate()
fmt.Println(err != nil) // true — запрос отвергнут, хотя всё верно
```

Интерфейс равен `nil`, только если пусты и тип, и значение. Возвращайте `nil` явно: `if len(errs) == 0 { return nil }`.

### Проверка до нормализации

```go
if !strings.Contains(u.Email, "@") { ... } // " Bob@X.io " проходит
u.Email = strings.ToLower(strings.TrimSpace(u.Email))
// а если нормализация в другом месте забыта — в базе два «разных» адреса
```

Сначала приводите данные к канонической форме, потом проверяйте и сохраняйте именно её. Иначе уникальный индекс по email не спасёт от `bob@x.io` и `Bob@x.io `.

### `url.Parse` и `mail.ParseAddress` слишком добрые

```go
url.Parse("javascript:alert(1)") // ошибки нет: Scheme "javascript"
url.Parse("example.com/a")       // ошибки нет: относительный путь, Host ""
mail.ParseAddress("Bob <bob@x.io>") // ошибки нет: Name "Bob", Address "bob@x.io"
```

Это парсеры, а не валидаторы поля формы. После `url.Parse` проверяйте `Scheme` по белому списку и непустой `Host`; после `ParseAddress` — что `Address` совпадает с исходной строкой. Ссылка со схемой `javascript:` в профиле пользователя — готовый XSS.

## Задание

Напишите валидацию запроса регистрации.

```go
type SignupRequest struct {
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Age      int      `json:"age"`
	Tags     []string `json:"tags"`
}

type FieldErrors map[string]string

func (fe FieldErrors) Error() string
func (r *SignupRequest) Normalize()
func (r SignupRequest) Validate() error
func Status(err error) int
```

`Normalize` приводит поля к канонической форме:

- `Username` — `strings.TrimSpace`; `Email` — `strings.TrimSpace`, затем `strings.ToLower`; `Password` и `Age` не меняются;
- каждый тег — `TrimSpace` и `ToLower`; пустые после этого теги удаляются, повторы тоже (остаётся первое вхождение, порядок сохраняется). Если тегов не осталось, `Tags` — пустой срез или `nil`, неважно.

`Validate` нормализацию не вызывает и проверяет поля как есть. Для каждого поля — **первое** нарушенное правило по порядку, длины — в символах (рунах):

- `username`: пустой → `"required"`; длина не от 3 до 20 → `"must be 3-20 characters"`; есть символы кроме латинских букв `a-z`, `A-Z`, цифр и `_` → `"invalid characters"`;
- `email`: пустой → `"required"`; `mail.ParseAddress` вернул ошибку или поле `Address` результата не равно строке целиком → `"invalid email"`;
- `password`: пустой → `"required"`; короче 8 символов → `"must be at least 8 characters"`;
- `age`: вне диапазона 13–120 (включительно) → `"must be between 13 and 120"`;
- `tags`: больше 5 тегов → ключ `"tags"`, сообщение `"at most 5 tags"`; кроме того, каждый тег длиннее 20 символов → ключ `"tags[i]"` (`i` — индекс тега в срезе), сообщение `"must be at most 20 characters"`.

Если ошибок нет, `Validate` возвращает `nil`, иначе — значение типа `FieldErrors` со всеми ошибками (ключи — имена полей в JSON, как выше).

`FieldErrors.Error()` — пары `ключ: сообщение`, отсортированные по ключу (`sort.Strings`) и соединённые через `"; "`: `"age: must be between 13 and 120; email: required"`.

`Status(err)` выбирает код ответа: `nil` → `200`; если в цепочке ошибки есть `FieldErrors` (в том числе обёрнутая через `%w` или внутри `errors.Join`) → `422`; любая другая ошибка → `400`.
