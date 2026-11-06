// `time.Time` — момент времени с наносекундной точностью
// и часовым поясом (`*time.Location`). `time.Duration` —
// промежуток, целое число наносекунд (`int64`).

package main

import (
	"fmt"
	"time"
)

func main() {
	// `time.Date` собирает момент из компонентов. Пояс —
	// обязательный аргумент: `time.UTC`, `time.Local` или
	// `time.FixedZone` со смещением в секундах.
	msk := time.FixedZone("MSK", 3*60*60)
	t := time.Date(2024, time.March, 15, 18, 30, 0, 0, msk)
	fmt.Println(t)
	fmt.Println(t.Year(), t.Month(), t.Day(), t.Weekday(), t.YearDay())

	// `Duration` печатается по-человечески. Константы
	// `time.Hour`, `time.Minute`… — это тоже `Duration`.
	d := 90*time.Minute + 30*time.Second
	fmt.Println(d, d.Hours(), d.Round(time.Hour))

	// Умножить `Duration` на переменную можно только
	// после явного приведения: `int * Duration` не сложится.
	n := 3
	fmt.Println(time.Duration(n) * time.Second)

	// `Add` прибавляет `Duration`, `Sub` возвращает разницу
	// двух моментов, `AddDate` — календарные годы, месяцы, дни.
	later := t.Add(36 * time.Hour)
	fmt.Println(later, later.Sub(t))
	fmt.Println(t.AddDate(0, 1, 0).Format(time.DateOnly))

	// `AddDate` нормализует дату: 31 января + 1 месяц =
	// «31 февраля» = 2 марта (в високосном 2024).
	jan31 := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	fmt.Println(jan31.AddDate(0, 1, 0).Format(time.DateOnly))

	// `Truncate` и `Round` выравнивают по кратности
	// `Duration` — отсчёт от нулевого времени, в UTC.
	fmt.Println(t.Truncate(time.Hour).Format(time.Kitchen))

	// Сравнивайте через `Equal`, `Before`, `After`, `Compare`.
	// `==` сравнивает ещё и пояс: один и тот же момент
	// в UTC и в MSK для `==` разный.
	u := t.In(time.UTC)
	fmt.Println(u, t == u, t.Equal(u), t.Before(later))

	// Нулевое значение — 1 января года 1 UTC. Это не
	// «нет даты», как `null`; проверяйте `IsZero`.
	var zero time.Time
	fmt.Println(zero, zero.IsZero())

	// `time.Now()` содержит ещё и показания монотонных
	// часов: `Sub` и `time.Since` по ним не врут, даже если
	// системные часы перевели. Печатать `Now` в примере
	// не будем — вывод должен быть одинаковым.
	start := time.Now()
	fmt.Println(time.Since(start) >= 0)
}
