---
title: Время
summary: time.Time и time.Duration, монотонные часы, Add/Sub/AddDate/Truncate, сравнение через Equal, часовые пояса и нулевое время.
difficulty: 3
minutes: 25
prerequisites:
  - 03-types/02-methods
  - 06-stdlib/02-formatting
docs:
  - title: "Пакет time"
    url: https://pkg.go.dev/time
---

В Go один тип для момента времени — `time.Time`. Он хранит момент с точностью до наносекунды и `*time.Location` — часовой пояс, в котором этот момент показывать. Это ближе к `DateTimeOffset`, чем к `DateTime`: путаницы `DateTimeKind.Unspecified` нет, у каждого значения пояс известен.

Промежуток — `time.Duration`, обычный `int64` наносекунд с методами. Константы `time.Second`, `time.Hour` — тоже `Duration`, поэтому `90*time.Minute` читается как текст и типобезопасно. `time.Time` — значение, а не ссылка: все методы (`Add`, `In`, `Truncate`) возвращают новый момент.

## Как это в C#

- `DateTime`/`DateTimeOffset` → `time.Time`; `TimeSpan` → `time.Duration` (наносекунды, а не тики по 100 нс). Диапазон `Duration` — примерно ±292 года.
- `DateTime.Now` и `Stopwatch` → `time.Now()`. Отдельный секундомер не нужен: `Now` содержит показания монотонных часов, и `time.Since(start)` не врёт при переводе системных часов.
- `TimeZoneInfo.FindSystemTimeZoneById` → `time.LoadLocation("Europe/Moscow")`. База поясов берётся из ОС; в минимальном контейнере её может не быть — тогда импортируйте `time/tzdata` или используйте `time.FixedZone`.
- `DateTime` сравнивается оператором `==` по тикам (даже `Kind` не учитывается), `DateTimeOffset` — по моменту в UTC. В Go `==` для `time.Time` сравнивает структуру целиком — с поясом и монотонными показаниями. Моменты сравнивают методами `Equal`, `Before`, `After`, `Compare`.

## Ловушки

### `==` вместо `Equal`

```go
a := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
b := a.In(time.FixedZone("MSK", 3*3600))
fmt.Println(a == b, a.Equal(b)) // false true
```

Один и тот же момент в разных поясах для `==` разный. По той же причине `time.Time` — плохой ключ map; используйте `t.UTC()` или `t.Unix()`.

### `Truncate(24 * time.Hour)` — не полночь

```go
t := time.Date(2024, 3, 15, 1, 30, 0, 0, time.FixedZone("MSK", 3*3600))
fmt.Println(t.Truncate(24 * time.Hour)) // 2024-03-14 03:00:00 +0300 MSK
```

`Truncate` и `Round` считают от нулевого момента в UTC и о поясе не знают. Начало суток в поясе — `time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())`.

### Нулевое время вместо `null`

```go
var deadline time.Time
if deadline.Before(time.Now()) { // true: 0001-01-01 давно прошло
	cancel()
}
```

У `time.Time` нет `null`. «Не задано» — это нулевое значение, и проверять его надо через `IsZero()`, иначе оно участвует в сравнениях как 1 января года 1.

## Задание

Напишите две функции для расчёта сроков.

`func AddWorkdays(t time.Time, n int) time.Time`:

- рабочие дни — с понедельника по пятницу; день недели определяется в поясе `t`;
- при `n > 0` возвращает `n`-й рабочий день после `t`, при `n < 0` — `|n|`-й рабочий день до `t`; день самого `t` не считается;
- время суток и пояс сохраняются как у `t`; дни отсчитываются календарно (`AddDate`), а не по 24 часа;
- при `n == 0` возвращает `t` без изменений, даже если это выходной.

Пример: от пятницы 10:00 `AddWorkdays(t, 1)` — понедельник 10:00, `AddWorkdays(t, -5)` — предыдущая пятница 10:00.

`func Overlap(aStart, aEnd, bStart, bEnd time.Time) time.Duration`:

- возвращает длительность пересечения полуинтервалов `[aStart, aEnd)` и `[bStart, bEnd)`;
- моменты могут быть в разных поясах — сравниваются сами моменты, а не показания часов;
- если интервалы не пересекаются, касаются концами или какой-то из них пустой либо «перевёрнутый» (конец не позже начала) — `0`.
