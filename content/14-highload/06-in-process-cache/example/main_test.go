package main

import (
	"fmt"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Фейковые часы: тест сдвигает время сам.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// Истёкшая запись — промах, даже если она ещё лежит в map. Удаляет
// её либо чтение (лениво), либо фоновая чистка.
func TestTTL(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	c := NewTTL[string, int](time.Minute, clk.Now)
	c.Set("apple", 10)
	c.Set("pear", 20)
	clk.Add(59 * time.Second)
	v, ok := c.Get("apple")
	t.Logf("через 59 с: apple = %d, %v", v, ok)
	clk.Add(time.Second)
	_, ok = c.Get("apple")
	t.Logf("через 60 с: apple найден: %v, записей в map: %d", ok, c.Len())
	t.Logf("фоновая чистка удалила: %d, записей в map: %d", c.Sweep(), c.Len())
	if ok || c.Len() != 0 {
		t.Fatal("истёкшие записи должны исчезнуть")
	}
}

// Горутина чистки получает тики из канала и останавливается по `stop`.
func TestJanitor(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	c := NewTTL[int, string](time.Second, clk.Now)
	for i := range 100 {
		c.Set(i, "x")
	}
	tick := make(chan time.Time)
	stop := c.StartJanitor(tick)
	clk.Add(time.Second)
	tick <- time.Time{} // горутина приняла тик и начала чистку
	stop()              // дождались её выхода
	t.Logf("после тика и остановки записей: %d", c.Len())
	if c.Len() != 0 {
		t.Fatal("чистка не удалила истёкшие записи")
	}
}

// Ёмкость 3: чтение `a` делает его свежим, и при переполнении
// вытесняется `b` — самый давно использованный.
func TestLRU(t *testing.T) {
	c := NewLRU[string, int](3)
	for i, k := range []string{"a", "b", "c"} {
		c.Set(k, i)
	}
	c.Get("a")
	ev, _ := c.Set("d", 3)
	t.Logf("вытеснен: %s", ev)
	ev, _ = c.Set("e", 4)
	t.Logf("вытеснен: %s", ev)
	_, okA := c.Get("a")
	_, okB := c.Get("b")
	t.Logf("a в кеше: %v, b в кеше: %v", okA, okB)
}

// Хеш раскладывает ключи по шардам почти поровну — иначе один
// горячий шард сведёт выигрыш на нет.
func TestShardDistribution(t *testing.T) {
	counts := make([]int, shards)
	for i := range 16000 {
		counts[shardOf("product:"+strconv.Itoa(i))]++
	}
	t.Logf("16000 ключей по %d шардам: от %d до %d в шарде", shards, slices.Min(counts), slices.Max(counts))
}

// Параллельное чтение: все горутины бьют в один мьютекс или
// расходятся по шестнадцати. Сравнивайте с `-cpu 1,4,8`:
// на одном ядре разницы почти нет, на многих она и видна.
func BenchmarkGetParallel(b *testing.B) {
	keys := make([]string, 1024)
	locked := &Locked[int]{m: make(map[string]int)}
	sharded := NewSharded[int]()
	for i := range keys {
		keys[i] = fmt.Sprintf("product:%d", i)
		locked.m[keys[i]] = i
		sharded.Set(keys[i], i)
	}
	b.Run("one-mutex", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for i := 0; pb.Next(); i++ {
				locked.Get(keys[i&1023])
			}
		})
	})
	b.Run("sharded", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for i := 0; pb.Next(); i++ {
				sharded.Get(keys[i&1023])
			}
		})
	})
}
