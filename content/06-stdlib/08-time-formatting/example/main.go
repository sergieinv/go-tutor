// Формат даты в Go задаётся не буквами `yyyy-MM-dd`,
// а примером: как бы выглядела эталонная дата
// `Mon Jan 2 15:04:05 MST 2006`. Её компоненты по порядку —
// 1 2 3 4 5 6 7: месяц 01, день 02, час 03 (15), минута 04,
// секунда 05, год (20)06, пояс -07(00).

package main

import (
	"fmt"
	"time"
)

func main() {
	t := time.Date(2024, time.March, 5, 15, 4, 9, 123_000_000,
		time.FixedZone("MSK", 3*3600))

	// Готовые раскладки — константы пакета.
	fmt.Println(t.Format(time.RFC3339))
	fmt.Println(t.Format(time.DateTime), "|", t.Format(time.Kitchen))

	// Свои раскладки собираются из кусков эталона:
	// `02.01.2006` — день.месяц.год, `Jan`/`January` —
	// имя месяца, `Mon` — день недели, `_2` — день
	// с пробелом вместо нуля, `.000` — миллисекунды,
	// `-07:00` — смещение пояса.
	fmt.Println(t.Format("02.01.2006 15:04"))
	fmt.Println(t.Format("Mon, _2 Jan 2006"))
	fmt.Println(t.Format("15:04:05.000 -07:00"))

	// `03` — час в 12-часовом формате, `PM` — AM/PM.
	fmt.Println(t.Format("03:04 PM"))

	// `Parse` разбирает строку по той же раскладке.
	// Без смещения в строке результат — в UTC.
	p, err := time.Parse("2006-01-02 15:04", "2024-03-05 10:00")
	fmt.Println(p, err)

	// `ParseInLocation` трактует такую строку в заданном
	// поясе — например, локальное время пользователя.
	p, _ = time.ParseInLocation("2006-01-02 15:04", "2024-03-05 10:00", t.Location())
	fmt.Println(p, p.UTC())

	// Ошибки разбора подробные: что и где не совпало.
	_, err = time.Parse(time.DateOnly, "05.03.2024")
	fmt.Println(err)
	_, err = time.Parse(time.DateOnly, "2024-02-30")
	fmt.Println(err)

	// Unix-время — секунды (или миллисекунды) от
	// 1970-01-01 UTC. От пояса не зависит.
	fmt.Println(t.Unix(), t.UnixMilli())

	// Обратно — `time.Unix(сек, нс)` и `time.UnixMilli`.
	// Результат в поясе `time.Local`; для стабильного вывода
	// переводим в UTC.
	fmt.Println(time.Unix(1_700_000_000, 0).UTC())
	fmt.Println(time.UnixMilli(t.UnixMilli()).Equal(t))
}
