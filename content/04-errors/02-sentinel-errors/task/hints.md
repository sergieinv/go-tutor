## Подсказка 1

Ошибки уже объявлены в шаблоне — возвращайте сами эти переменные, а не `errors.New` с тем же текстом: новая ошибка не пройдёт проверку `errors.Is`.

## Подсказка 2

Проверяйте по порядку и выходите сразу: сумма, затем наличие счёта, затем баланс. Наличие счёта проверяет форма `balance, ok := balances[account]` — она работает и для `nil`-map. Записывайте в map только в самом конце, когда все проверки пройдены.

## Подсказка 3

```go
if amount <= 0 {
	return ErrInvalidAmount
}
balance, ok := balances[account]
if !ok {
	return fmt.Errorf("account %q: %w", account, ErrNoAccount)
}
```

Аналогично для `ErrInsufficientFunds`, затем `balances[account] = balance - amount`.
