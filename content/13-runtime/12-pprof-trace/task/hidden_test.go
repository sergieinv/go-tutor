package task

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
)

func TestBlockedEmptyProfile(t *testing.T) {
	got, err := Blocked("goroutine profile: total 0\n", "main")
	if err != nil || len(got) != 0 {
		t.Fatalf("Blocked() = %v, %v, want пусто и nil", got, err)
	}
}

func TestBlockedNoMatches(t *testing.T) {
	got, err := Blocked(sample, "example.com/none")
	if err != nil || len(got) != 0 {
		t.Fatalf("Blocked() = %v, %v, want пусто и nil", got, err)
	}
}

func TestBlockedPrefixIsPackageNotString(t *testing.T) {
	p := `goroutine profile: total 4
2 @ 0x1
#	0x1	mainx.Run+0x1	/a.go:1
#	0x2	main.run+0x2	/a.go:2

2 @ 0x1
#	0x1	example.com/app/worker.Loop+0x1	/w.go:1
#	0x2	example.com/app.Start+0x2	/a.go:3
`
	got, err := Blocked(p, "main")
	if err != nil {
		t.Fatal(err)
	}
	if want := []Site{{"main.run", 2}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Blocked(main) = %v, want %v — mainx.Run из другого пакета", got, want)
	}
	got, err = Blocked(p, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	if want := []Site{{"example.com/app.Start", 2}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Blocked(example.com/app) = %v, want %v — worker — другой пакет", got, want)
	}
}

func TestBlockedSumsAndSorts(t *testing.T) {
	p := `goroutine profile: total 13
4 @ 0x1
#	0x1	app.b+0x1	/a.go:1

4 @ 0x1
#	0x1	app.a+0x1	/a.go:1

3 @ 0x1
#	0x1	app.c+0x10	/a.go:9
#	0x2	app.main+0x2	/a.go:2

2 @ 0x1
#	0x1	app.c+0x20	/a.go:7
`
	got, err := Blocked(p, "app")
	if err != nil {
		t.Fatal(err)
	}
	// c: 3 + 2 из двух разных групп; a и b по 4 — по имени.
	want := []Site{{"app.c", 5}, {"app.a", 4}, {"app.b", 4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Blocked() = %v, want %v", got, want)
	}
}

func TestBlockedBadFormat(t *testing.T) {
	cases := map[string]string{
		"пустая строка":         "",
		"нет заголовка":         "3 @ 0x1\n#\t0x1\tmain.f+0x1\t/a.go:1\n",
		"кадр до группы":        "goroutine profile: total 1\n#\t0x1\tmain.f+0x1\t/a.go:1\n",
		"кадр после пустой":     "goroutine profile: total 1\n1 @ 0x1\n\n#\t0x1\tmain.f+0x1\t/a.go:1\n",
		"число не число":        "goroutine profile: total 1\nx @ 0x1\n#\t0x1\tmain.f+0x1\t/a.go:1\n",
		"ноль горутин":          "goroutine profile: total 0\n0 @ 0x1\n#\t0x1\tmain.f+0x1\t/a.go:1\n",
		"нет @":                 "goroutine profile: total 1\n1\n#\t0x1\tmain.f+0x1\t/a.go:1\n",
		"кадр без функции":      "goroutine profile: total 1\n1 @ 0x1\n#\t0x1\n",
		"заголовок не в начале": "\ngoroutine profile: total 1\n",
	}
	for name, p := range cases {
		if got, err := Blocked(p, "main"); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: Blocked() = %v, %v, want ErrFormat", name, got, err)
		}
	}
}

//go:noinline
func parkOnChan(started *sync.WaitGroup, ch <-chan struct{}) {
	started.Done()
	<-ch
}

//go:noinline
func parkOnMutex(started *sync.WaitGroup, mu *sync.Mutex) {
	started.Done()
	mu.Lock()
	mu.Unlock()
}

// Настоящий профиль этого процесса: 5 горутин стоят в parkOnChan,
// 3 — в parkOnMutex (у части есть метки pprof.Do).
func TestBlockedRealProfile(t *testing.T) {
	name := runtime.FuncForPC(reflect.ValueOf(parkOnChan).Pointer()).Name()
	pkg := name[:strings.LastIndex(name, ".")]

	ch := make(chan struct{})
	var mu sync.Mutex
	mu.Lock()
	var started, done sync.WaitGroup
	for range 5 {
		started.Add(1)
		done.Go(func() { parkOnChan(&started, ch) })
	}
	for i := range 3 {
		started.Add(1)
		done.Go(func() {
			if i == 0 {
				parkOnMutex(&started, &mu)
				return
			}
			pprof.Do(context.Background(), pprof.Labels("job", "import"), func(context.Context) {
				parkOnMutex(&started, &mu)
			})
		})
	}
	started.Wait() // все горутины уже внутри своих функций
	defer func() {
		close(ch)
		mu.Unlock()
		done.Wait()
	}()

	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 1); err != nil {
		t.Fatal(err)
	}
	got, err := Blocked(buf.String(), pkg)
	if err != nil {
		t.Fatalf("Blocked: %v\n%s", err, buf.String())
	}
	counts := map[string]int{}
	for _, s := range got {
		counts[s.Func] = s.Count
	}
	if counts[pkg+".parkOnChan"] != 5 || counts[pkg+".parkOnMutex"] != 3 {
		t.Fatalf("Blocked() = %v\nwant parkOnChan: 5, parkOnMutex: 3\nпрофиль:\n%s", got, buf.String())
	}
	if len(got) == 0 || got[0] != (Site{pkg + ".parkOnChan", 5}) {
		t.Fatalf("первым должен идти parkOnChan с 5 горутинами: %v", got)
	}
}
