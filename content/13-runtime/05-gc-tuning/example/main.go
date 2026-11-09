// Две ручки сборщика мусора — `GOGC` (во сколько раз дать
// куче вырасти между циклами) и `GOMEMLIMIT` (мягкий лимит
// памяти). Их можно задать переменными окружения или из кода
// через `runtime/debug`. Пример меняет их на ходу и читает
// результат из `runtime/metrics`. С `GODEBUG=gctrace=1` в stderr
// видно, как меняются цель (`MB goal`) и частота сборок.

package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
)

const MiB = 1 << 20

// `metric` читает одно значение. `metrics.Read` не
// останавливает мир, в отличие от `runtime.ReadMemStats`.
func metric(name string) uint64 {
	s := []metrics.Sample{{Name: name}}
	metrics.Read(s)
	return s[0].Value.Uint64()
}

var (
	live [][]byte // данные, которые программа держит
	sink []byte   // сюда пишется мусор
)

// `churn` выделяет `n` МиБ мусора по мегабайту.
func churn(n int) {
	for range n {
		sink = make([]byte, MiB)
	}
}

func main() {
	// По умолчанию GOGC=100, а лимита нет (`math.MaxInt64`).
	// Песочница запускает программы с `GOMEMLIMIT`, поэтому
	// лимит здесь задан: `/gc/gomemlimit:bytes`.
	fmt.Println("GOGC:", metric("/gc/gogc:percent"))

	// 32 МиБ живых данных. После сборки цель кучи —
	// живая куча плюс GOGC процентов от неё (и от стеков
	// с глобальными), то есть около 64 МиБ.
	for range 32 {
		live = append(live, make([]byte, MiB))
	}
	runtime.GC()
	heapLive := metric("/gc/heap/live:bytes")
	goal100 := metric("/gc/heap/goal:bytes")
	fmt.Println("цель ≈ 2 × живая куча:", goal100 > 2*heapLive-MiB && goal100 < 2*heapLive+2*MiB)

	// `SetGCPercent` пересчитывает цель сразу. Меньше
	// GOGC — меньше памяти, но сборки чаще.
	debug.SetGCPercent(50)
	goal50 := metric("/gc/heap/goal:bytes")
	debug.SetGCPercent(200)
	goal200 := metric("/gc/heap/goal:bytes")
	fmt.Println("цель растёт с GOGC:", goal50 < goal100 && goal100 < goal200)

	// GOGC=off: сборщик не запускается совсем, и куча
	// растёт на весь выделенный мусор.
	debug.SetGCPercent(-1)
	cycles := metric("/gc/cycles/total:gc-cycles")
	churn(64)
	fmt.Println("сборок при GOGC=off:", metric("/gc/cycles/total:gc-cycles")-cycles)

	// GOGC=off + лимит: сборок нет, пока память не подошла
	// к лимиту, дальше их ровно столько, чтобы в нём
	// остаться. Живые 32 МиБ при лимите 48 — тесно: сборки
	// идут почти на каждый новый мегабайт.
	debug.SetMemoryLimit(48 * MiB)
	cycles = metric("/gc/cycles/total:gc-cycles")
	churn(256)
	fmt.Println("сборки пошли:", metric("/gc/cycles/total:gc-cycles")-cycles > 0)

	// Лимит считает всю память рантайма, а не только кучу:
	// total минус возвращённое ОС.
	used := metric("/memory/classes/total:bytes") - metric("/memory/classes/heap/released:bytes")
	fmt.Println("из 256 МиБ мусора в памяти осталось меньше 64 МиБ:", used < 64*MiB)

	runtime.KeepAlive(live)
}
