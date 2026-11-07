## Подсказка 1

`testing.TB` — интерфейс с теми же методами, что у `*testing.T`: `Errorf`, `Fatalf`, `Skipf`, `Logf`… Пишите проверки так, как писали бы тело теста, только вместо `t` — параметр `tb`. Переменную окружения читает `os.Getenv`: для незаданной она возвращает `""`.

## Подсказка 2

`Fatalf` и `Skipf` сами останавливают тест (через `runtime.Goexit`), поэтому после них код не выполнится — `return` не обязателен. `Errorf` тест не останавливает, поэтому в `CheckDiscountError` ветки «ошибки нет» и «ошибка не та» должны исключать друг друга: `switch` с условиями или `if … else if`.

## Подсказка 3

```go
got, err := discount(price, percent)
if err != nil {
	tb.Fatalf("Discount(%d, %d): неожиданная ошибка: %v", price, percent, err)
}
if got != want {
	tb.Errorf("Discount(%d, %d) = %d, want %d", price, percent, got, want)
}
```

Ошибку сравнивайте через `errors.Is(err, target)`, а не `err == target`: реализация может обернуть её через `%w`.
