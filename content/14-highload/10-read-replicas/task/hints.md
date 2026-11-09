## Подсказка 1

`ParseLSN`: разрежьте строку `strings.Cut(s, "/")`, проверьте, что обе части — от 1 до 8 символов и каждый символ — шестнадцатеричная цифра (так отсекаются `+`, `-`, пробелы и `0x`, которые иначе пропустил бы `strconv`), и только потом `strconv.ParseUint(part, 16, 32)`. Результат — `LSN(hi<<32 | lo)`. `String` — `fmt.Sprintf("%X/%X", hi, lo)`.

## Подсказка 2

Номер вызова для кругового обхода — общий счётчик, к которому одновременно обращаются многие горутины: `atomic.Uint64` в структуре `Router`. `k := r.next.Add(1) - 1` выдаёт каждому вызову свой номер, а `start := int(k % uint64(n))` — стартовую реплику. Обход: `for i := range n { rep := r.replicas[(start+i)%n] … }`.

## Подсказка 3

Тело цикла:

```go
if token > 0 {
	lsn, err := rep.ReplayLSN(ctx)
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if err != nil || lsn < token {
		continue
	}
}
orders, err := rep.History(ctx, customerID)
if ctx.Err() != nil {
	return nil, false, ctx.Err()
}
if err == nil {
	return orders, true, nil
}
```

После цикла — основной. Не забудьте проверку `ctx.Err()` в самом начале и случай `n == 0` (деление на ноль в `k % n`).
