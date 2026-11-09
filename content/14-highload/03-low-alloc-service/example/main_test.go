package main

import (
	"bytes"
	"io"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"testing"
)

var order = Order{ID: 42, Customer: `Анна "VIP"`, Items: []Item{
	{SKU: "A-1", Qty: 2, Price: 1999},
	{SKU: "B-7", Qty: 1, Price: 54900},
}}

// Пул и счёт аллокаций предсказуемы на одном P и без фоновых
// сборок мусора — тест выключает их на время работы.
func quiet(t *testing.T) {
	procs := runtime.GOMAXPROCS(1)
	gc := debug.SetGCPercent(-1)
	t.Cleanup(func() {
		runtime.GOMAXPROCS(procs)
		debug.SetGCPercent(gc)
	})
}

// Ручной JSON обязан совпадать с `encoding/json` байт в байт —
// иначе это не оптимизация, а другой API.
func TestSameJSON(t *testing.T) {
	var slow, typed, fast bytes.Buffer
	if err := renderSlow(&slow, &order); err != nil {
		t.Fatal(err)
	}
	if err := renderTyped(&typed, &order); err != nil {
		t.Fatal(err)
	}
	if err := renderFast(&fast, &order); err != nil {
		t.Fatal(err)
	}
	t.Logf("ответ: %s", bytes.TrimSpace(fast.Bytes()))
	if slow.String() != fast.String() || typed.String() != fast.String() {
		t.Fatalf("slow = %s\ntyped = %s\nfast = %s", slow.String(), typed.String(), fast.String())
	}
	if s, f := statusSlow(&order), string(statusFast(nil, &order)); s != f {
		t.Fatalf("statusSlow = %q, statusFast = %q", s, f)
	}
}

// Сами числа аллокаций у `encoding/json` зависят от версии Go,
// поэтому печатаем сравнения, а точное число — только у ручной
// версии: там его гарантирует код.
func TestAllocs(t *testing.T) {
	quiet(t)
	slow := testing.AllocsPerRun(100, func() { renderSlow(io.Discard, &order) })
	typed := testing.AllocsPerRun(100, func() { renderTyped(io.Discard, &order) })
	fast := testing.AllocsPerRun(100, func() { renderFast(io.Discard, &order) })
	t.Logf("map[string]any выделяет больше, чем структура: %v", slow > typed)
	t.Logf("renderFast: %.0f аллокаций", fast)
	if fast != 0 || slow <= typed || typed < fast {
		t.Fatalf("slow %.0f, typed %.0f, fast %.0f", slow, typed, fast)
	}

	buf := make([]byte, 0, 64)
	sprintf := testing.AllocsPerRun(100, func() { _ = statusSlow(&order) })
	appendInt := testing.AllocsPerRun(100, func() { buf = statusFast(buf[:0], &order) })
	t.Logf("fmt.Sprintf выделяет память: %v; strconv.AppendInt: %.0f аллокаций", sprintf > 0, appendInt)
	if appendInt != 0 || sprintf <= appendInt {
		t.Fatalf("sprintf %.0f, appendInt %.0f", sprintf, appendInt)
	}
}

// Глобальные «стоки»: без них компилятор положил бы локальный
// срез на стек, и аллокаций не было бы вовсе.
var (
	sinkItems []Item
	sinkMap   map[int64]int
)

// Предразмер: `append` в пустой срез растёт ступенями и на каждой
// копирует данные, а `make(.., 0, n)` выделяет память один раз.
// С map то же: подсказка размера избавляет от роста таблиц.
func TestPresize(t *testing.T) {
	quiet(t)
	const n = 1000
	grow := testing.AllocsPerRun(10, func() {
		var s []Item
		for i := range n {
			s = append(s, Item{Qty: i})
		}
		sinkItems = s // срез уходит из функции, как ответ обработчика
	})
	presized := testing.AllocsPerRun(10, func() {
		s := make([]Item, 0, n)
		for i := range n {
			s = append(s, Item{Qty: i})
		}
		sinkItems = s
	})
	t.Logf("срез: без предразмера больше аллокаций: %v; с предразмером: %.0f", grow > presized, presized)

	mapGrow := testing.AllocsPerRun(10, func() {
		m := map[int64]int{}
		for i := range n {
			m[int64(i)] = i
		}
		sinkMap = m
	})
	mapHint := testing.AllocsPerRun(10, func() {
		m := make(map[int64]int, n)
		for i := range n {
			m[int64(i)] = i
		}
		sinkMap = m
	})
	t.Logf("map: подсказка размера уменьшает число аллокаций: %v", mapHint < mapGrow)
	if presized != 1 || grow <= presized || mapHint >= mapGrow {
		t.Fatalf("срез %.0f/%.0f, map %.0f/%.0f", grow, presized, mapGrow, mapHint)
	}
}

// Обработчик целиком: заголовки, `ResponseWriter` и сам ответ.
func TestHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	handleOrder(&order)(rec, httptest.NewRequest("GET", "/orders/42", nil))
	t.Logf("статус %d, %s", rec.Code, rec.Header().Get("Content-Type"))
	if rec.Code != 200 || rec.Body.Len() == 0 {
		t.Fatalf("статус %d, тело %q", rec.Code, rec.Body.String())
	}
}

// Бенчмарки: сравните B/op и allocs/op. Параллельный вариант
// показывает, что пул масштабируется: у каждого P своя часть.
func BenchmarkRender(b *testing.B) {
	b.Run("slow", func(b *testing.B) {
		for b.Loop() {
			renderSlow(io.Discard, &order)
		}
	})
	b.Run("typed", func(b *testing.B) {
		for b.Loop() {
			renderTyped(io.Discard, &order)
		}
	})
	b.Run("fast", func(b *testing.B) {
		for b.Loop() {
			renderFast(io.Discard, &order)
		}
	})
	b.Run("fast-parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				renderFast(io.Discard, &order)
			}
		})
	})
}

func BenchmarkStatus(b *testing.B) {
	b.Run("sprintf", func(b *testing.B) {
		for b.Loop() {
			_ = statusSlow(&order)
		}
	})
	b.Run("append", func(b *testing.B) {
		buf := make([]byte, 0, 64)
		for b.Loop() {
			buf = statusFast(buf[:0], &order)
		}
	})
}
