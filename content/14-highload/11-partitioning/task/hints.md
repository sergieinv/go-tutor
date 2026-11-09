## Подсказка 1

Начало месяца в UTC: `t = t.UTC(); time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)`. `Monthly` идёт от начала месяца, в котором лежит `from`, шагом `m.AddDate(0, 1, 0)`, пока `m.Before(to)`: так месяц, в котором `to` стоит ровно на 1-м числе 00:00, не попадает в список. Имя — `fmt.Sprintf("%s_%04d_%02d", parent, m.Year(), int(m.Month()))`.

## Подсказка 2

Имя проверяйте побайтно: `a-z` и `_` можно везде, `0-9` — кроме первой позиции, длина — от 1 до предела (55 для `Monthly`, 63 для DDL). Так в DDL не попадут ни кавычки, ни `;`, ни пробелы — плейсхолдеров для имён таблиц нет. Литерал времени — `t.UTC().Format("2006-01-02 15:04:05") + "+00"`: без смещения PostgreSQL прочтёт границу в поясе сессии.

## Подсказка 3

`Locate` — первая партиция с `!t.Before(p.From) && t.Before(p.To)`. `Expired`:

```go
keep = max(keep, 0)
cutoff := monthStart(now).AddDate(0, -keep, 0)
for _, p := range parts {
	if !p.To.After(cutoff) { // To <= cutoff
		out = append(out, p)
	}
}
```

Сравнивайте `time.Time` методами `Before`/`After`/`Equal`, а не `==`: у моментов в разных поясах разные поля.
