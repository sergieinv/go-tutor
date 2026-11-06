## Подсказка 1

`errors.Is` спускается по цепочке через метод `Unwrap() error`. У `*FieldError` причина лежит в поле `Err` — метод просто возвращает его.

## Подсказка 2

Собирайте ошибки в срез `[]error` и в конце верните `errors.Join(errs...)`. Если срез пустой, `Join` сам вернёт `nil`. Для каждого поля используйте `switch` без выражения: первая сработавшая ветка — единственная ошибка поля.

## Подсказка 3

```go
func (e *FieldError) Unwrap() error { return e.Err }
```

Проверка почты — `strings.Contains(u.Email, "@")`, длина имени в байтах — `len(u.Name) > 32`. Ошибку добавляйте как `&FieldError{Field: "email", Err: ErrInvalid}`.
