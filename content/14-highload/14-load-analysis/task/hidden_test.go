package task

import (
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"testing/iotest"
)

func TestHiddenParseLabelEscapes(t *testing.T) {
	got := mustParse(t, `http_requests_total{path="/a\"b\\c\nd",method="GET"} 3`+"\n")
	if len(got) != 1 {
		t.Fatalf("Parse: %d samples, want 1", len(got))
	}
	if p := got[0].Labels["path"]; p != "/a\"b\\c\nd" {
		t.Errorf(`label path = %q, want "/a\"b\\c\nd"`, p)
	}
	if m := got[0].Labels["method"]; m != "GET" {
		t.Errorf("label method = %q, want GET", m)
	}
	// Запятая, «}» и «=» внутри кавычек — часть значения.
	got = mustParse(t, `x{q="a,b}=c"} 1`)
	if len(got) != 1 || got[0].Labels["q"] != "a,b}=c" {
		t.Errorf(`Parse(x{q="a,b}=c"} 1) = %+v, want label q = "a,b}=c"`, got)
	}
}

func TestHiddenParseLabelSyntax(t *testing.T) {
	ok := map[string]int{ // вход → число меток
		`x{} 1`:                     0,
		`x{a="1",} 1`:               1, // висячая запятая допустима
		`x{a="1", b="2"} 1`:         2,
		"x{ a=\"1\" ,\tb=\"2\" } 1": 2,
		`x{a=""} 1`:                 0, // пустое значение — как отсутствие метки
		`x{a="",b="2"} 1`:           1,
		`x{_a1="1"} 1`:              1,
	}
	for in, n := range ok {
		got, err := Parse(strings.NewReader(in))
		if err != nil || len(got) != 1 {
			t.Errorf("Parse(%q) = %+v, %v; want one sample", in, got, err)
			continue
		}
		if got[0].Labels == nil || len(got[0].Labels) != n {
			t.Errorf("Parse(%q) labels = %#v, want %d labels (non-nil map)", in, got[0].Labels, n)
		}
		if _, has := got[0].Labels["a"]; has && got[0].Labels["a"] == "" {
			t.Errorf("Parse(%q) keeps empty label a", in)
		}
	}
	bad := []string{
		`x{a="1"`,          // нет «}»
		`x{a="1"} `,        // нет значения
		`x{a=1} 1`,         // значение без кавычек
		`x{a="1"b="2"} 1`,  // нет запятой
		`x{,} 1`,           // пустой элемент
		`x{1a="1"} 1`,      // имя метки с цифры
		`x{a:b="1"} 1`,     // двоеточие в имени метки нельзя
		`x{a-b="1"} 1`,     // дефис
		`x{a="1",a="2"} 1`, // повтор метки
		`x{a="",a="2"} 1`,  // повтор и с пустым значением
		`x{a="\t"} 1`,      // неизвестная escape-последовательность
		`x{a="\"} 1`,       // экранированная кавычка не закрывает значение
		`x{a="1"}1`,        // нет пробела перед значением
	}
	for _, in := range bad {
		if got, err := Parse(strings.NewReader(in)); !errors.Is(err, ErrSyntax) {
			t.Errorf("Parse(%q) = %+v, %v; want ErrSyntax", in, got, err)
		}
	}
}

func TestHiddenParseNamesValuesTimestamps(t *testing.T) {
	got := mustParse(t, strings.Join([]string{
		"ns:sub_total 1.5e3",
		"_x 0",
		"a NaN",
		"b +Inf",
		"c -Inf",
		"d 7 1700000000000", // метка времени игнорируется
		"e\t-0.25\t-5",      // табы и отрицательная метка времени
	}, "\n"))
	if len(got) != 7 {
		t.Fatalf("Parse: %d samples, want 7", len(got))
	}
	if got[0].Name != "ns:sub_total" || got[0].Value != 1500 {
		t.Errorf("sample 0 = %+v, want ns:sub_total 1500", got[0])
	}
	if !math.IsNaN(got[2].Value) || !math.IsInf(got[3].Value, 1) || !math.IsInf(got[4].Value, -1) {
		t.Errorf("NaN/+Inf/-Inf parsed as %v %v %v", got[2].Value, got[3].Value, got[4].Value)
	}
	if got[5].Value != 7 || got[6].Name != "e" || got[6].Value != -0.25 {
		t.Errorf("samples 5, 6 = %+v %+v; want d 7 and e -0.25", got[5], got[6])
	}
	for _, in := range []string{
		"1abc 1",    // имя с цифры
		"a-b 1",     // дефис в имени
		"x",         // нет значения
		"x 1 2 3",   // лишнее поле
		"x 1 1.5",   // метка времени не целая
		"x 1 abc",   // метка времени не число
		"x 1,5",     // запятая вместо точки
		`{a="1"} 1`, // нет имени
		"x 0x",      // битое число
	} {
		if got, err := Parse(strings.NewReader(in)); !errors.Is(err, ErrSyntax) {
			t.Errorf("Parse(%q) = %+v, %v; want ErrSyntax", in, got, err)
		}
	}
}

func TestHiddenParseCommentsAndBlankLines(t *testing.T) {
	in := "# HELP x Help with \\n escaped \"quotes\" and {braces}\n" +
		"   # TYPE x counter\n" +
		"#no space after hash\n" +
		"\n" +
		" \t \n" +
		"  x 1  \n" +
		"# x 999\n" +
		"y{a=\"#\"} 2\n"
	got := mustParse(t, in)
	if len(got) != 2 || got[0].Name != "x" || got[0].Value != 1 || got[1].Labels["a"] != "#" {
		t.Errorf("Parse = %+v, want x 1 and y{a=\"#\"} 2", got)
	}
	empty, err := Parse(strings.NewReader(""))
	if err != nil || len(empty) != 0 {
		t.Errorf(`Parse("") = %+v, %v; want no samples, nil`, empty, err)
	}
}

func TestHiddenParseErrorHasLineNumber(t *testing.T) {
	in := "# TYPE x counter\nx 1\n\nx{a=\"2\"} oops\n"
	_, err := Parse(strings.NewReader(in))
	if !errors.Is(err, ErrSyntax) || !strings.Contains(err.Error(), "line 4") {
		t.Errorf("Parse error = %v, want ErrSyntax mentioning line 4", err)
	}
}

func TestHiddenParseDuplicateSeries(t *testing.T) {
	for _, in := range []string{
		"x 1\nx 2\n",
		"x{a=\"1\",b=\"2\"} 1\nx{b=\"2\",a=\"1\"} 2\n", // порядок меток не важен
		"x{a=\"\"} 1\nx 2\n",                           // пустая метка — тот же ряд
	} {
		_, err := Parse(strings.NewReader(in))
		if !errors.Is(err, ErrSyntax) || !strings.Contains(err.Error(), "line 2") {
			t.Errorf("Parse(%q) error = %v, want ErrSyntax mentioning line 2", in, err)
		}
	}
	// Разные метки или разные имена — разные ряды.
	got := mustParse(t, "x{a=\"1\"} 1\nx{a=\"2\"} 2\nx 3\ny 4\n")
	if len(got) != 4 {
		t.Errorf("Parse: %d samples, want 4", len(got))
	}
}

// infinite — бесконечный вход, считает прочитанные байты.
type infinite struct{ n int64 }

func (r *infinite) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = '#'
	}
	r.n += int64(len(p))
	return len(p), nil
}

func TestHiddenParseSizeLimit(t *testing.T) {
	line := "# padding padding padding padding padding padding padding padding\n"
	exact := strings.Repeat(line, MaxInput/len(line))
	exact += "x 1" + strings.Repeat(" ", MaxInput-len(exact)-3)
	if len(exact) != MaxInput {
		t.Fatalf("test bug: len %d", len(exact))
	}
	got, err := Parse(strings.NewReader(exact))
	if err != nil || len(got) != 1 {
		t.Errorf("Parse(exactly MaxInput bytes) = %d samples, %v; want 1 sample", len(got), err)
	}
	if _, err := Parse(strings.NewReader(exact + "\n")); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Parse(MaxInput+1 bytes) error = %v, want ErrTooLarge", err)
	}
	// Синтаксическая ошибка в начале не отменяет проверку размера.
	big := "broken line\n" + strings.Repeat(line, MaxInput/len(line)+1)
	if _, err := Parse(strings.NewReader(big)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Parse(too large with broken first line) error = %v, want ErrTooLarge", err)
	}
	r := &infinite{}
	if _, err := Parse(r); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Parse(infinite) error = %v, want ErrTooLarge", err)
	}
	if r.n > MaxInput+64<<10 {
		t.Errorf("Parse read %d bytes from an endless reader, want about MaxInput+1", r.n)
	}
}

func TestHiddenParseReadError(t *testing.T) {
	boom := errors.New("connection reset")
	_, err := Parse(io.MultiReader(strings.NewReader("x 1\n"), iotest.ErrReader(boom)))
	if !errors.Is(err, boom) {
		t.Errorf("Parse error = %v, want the reader's error", err)
	}
}

func s(name string, v float64, kv ...string) Sample {
	l := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		l[kv[i]] = kv[i+1]
	}
	return Sample{Name: name, Labels: l, Value: v}
}

func TestHiddenIncreaseResetAndSeries(t *testing.T) {
	before := []Sample{
		s("c", 100, "k", "a"),
		s("c", 50, "k", "b"),
		s("c", 10, "k", "gone"),
		s("c", 7, "k", "a", "extra", "1"),
		s("other", 1000),
	}
	after := []Sample{
		s("c", 20, "k", "a"),              // сброс: 100 → 20, считаем 20
		s("c", 70, "k", "b"),              // +20
		s("c", 5, "k", "new"),             // новый ряд: +5
		s("c", 9, "k", "a", "extra", "1"), // свой ряд, а не {k=a}: +2
		s("other", 0),
	}
	if got := Increase(before, after, "c", nil); !near(got, 47) {
		t.Errorf("Increase(c) = %v, want 47 (20 after reset + 20 + 5 new + 2; vanished series ignored)", got)
	}
	if got := Increase(before, after, "c", map[string]string{"k": "a"}); !near(got, 22) {
		t.Errorf("Increase(c{k=a}) = %v, want 22 (both series with k=a)", got)
	}
	if got := Increase(before, after, "c", map[string]string{"k": "a", "extra": ""}); !near(got, 20) {
		t.Errorf(`Increase(c{k=a,extra=""}) = %v, want 20 (empty value means the label is absent)`, got)
	}
	if got := Increase(before, after, "c", map[string]string{"k": "gone"}); got != 0 {
		t.Errorf("Increase(c{k=gone}) = %v, want 0 (series vanished)", got)
	}
	if got := Increase(before, after, "c", map[string]string{"nope": "x"}); got != 0 {
		t.Errorf("Increase(c{nope=x}) = %v, want 0", got)
	}
	if got := Increase(before, after, "other", nil); got != 0 {
		t.Errorf("Increase(other) = %v, want 0 (reset to 0)", got)
	}
	// Имя сравнивается точно: c_total — не c.
	if got := Increase(nil, []Sample{s("c_total", 3), s("c", 1)}, "c", nil); got != 1 {
		t.Errorf("Increase(c) with c_total present = %v, want 1", got)
	}
}

func TestHiddenIncreaseEmptyLabelEqualsAbsent(t *testing.T) {
	before := []Sample{s("c", 10, "a", "")}
	after := []Sample{s("c", 15)}
	if got := Increase(before, after, "c", nil); !near(got, 5) {
		t.Errorf(`Increase(c{a=""}=10 → c=15) = %v, want 5: the same series`, got)
	}
}

func TestHiddenIncreaseSkipsNonFinite(t *testing.T) {
	before := []Sample{s("c", math.NaN(), "k", "1"), s("c", 5, "k", "2"), s("c", 1, "k", "3"), s("c", 1, "k", "4"), s("c", -3, "k", "5")}
	after := []Sample{
		s("c", 10, "k", "1"),           // было NaN
		s("c", math.Inf(1), "k", "2"),  // стало +Inf
		s("c", math.NaN(), "k", "3"),   // стало NaN
		s("c", 4, "k", "4"),            // нормальный: +3
		s("c", 2, "k", "5"),            // было отрицательным
		s("c", math.Inf(-1), "k", "6"), // новый, но -Inf
		s("c", -1, "k", "7"),           // новый, но отрицательный
	}
	got := Increase(before, after, "c", nil)
	if math.IsNaN(got) || math.IsInf(got, 0) || !near(got, 3) {
		t.Errorf("Increase = %v, want 3 (series with NaN, ±Inf or negative values are skipped)", got)
	}
}

func TestHiddenSummarizeOverloadAndRestart(t *testing.T) {
	before := mustParse(t, `
orders_catalog_db_queries_total 500
grpc_server_handled_total{grpc_code="OK",grpc_method="CreateOrder",grpc_service="orders.v1.OrderService"} 2346
grpc_server_handled_total{grpc_code="OK",grpc_method="CreateOrder",grpc_service="warehouse.v1.WarehouseService"} 10
`)
	// Между снимками «Заказы» перезапустились: счётчики начались с нуля.
	after := mustParse(t, `
orders_catalog_db_queries_total 12
grpc_server_handled_total{grpc_code="OK",grpc_method="CreateOrder",grpc_service="orders.v1.OrderService"} 1072
grpc_server_handled_total{grpc_code="ResourceExhausted",grpc_method="CreateOrder",grpc_service="orders.v1.OrderService"} 3428
grpc_server_handled_total{grpc_code="OK",grpc_method="CreateOrder",grpc_service="warehouse.v1.WarehouseService"} 99
`)
	got, err := Summarize(before, after, 3)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	checkSummary(t, got, Summary{
		DBQueries: 12, DBPerSecond: 4,
		CreateCalls: 4500, Shed: 3428, ShedRatio: 3428.0 / 4500,
	})
}

func TestHiddenSummarizeEmptyAndErrorsIgnored(t *testing.T) {
	got, err := Summarize(nil, nil, 1)
	if err != nil {
		t.Fatalf("Summarize(nil, nil, 1): %v", err)
	}
	checkSummary(t, got, Summary{})
	// error и bypass не входят в обращения к кешу; reason не важен.
	after := mustParse(t, `
orders_catalog_cache_lookups_total{result="error"} 40
orders_catalog_cache_lookups_total{result="bypass"} 60
orders_catalog_cache_lookups_total{result="hit"} 3
orders_catalog_cache_lookups_total{result="miss"} 1
orders_history_reads_total{reason="replica_error",target="primary"} 5
orders_history_reads_total{reason="caught_up",target="replica"} 15
`)
	got, err = Summarize(nil, after, 2)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	checkSummary(t, got, Summary{
		CacheHits: 3, CacheLookups: 4, HitRatio: 0.75,
		ReplicaReads: 15, HistoryReads: 20, ReplicaRatio: 0.75,
	})
}

func TestHiddenSummarizeBadDuration(t *testing.T) {
	for _, d := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := Summarize(nil, nil, d); !errors.Is(err, ErrDuration) {
			t.Errorf("Summarize(seconds=%v) error = %v, want ErrDuration", d, err)
		}
	}
}
