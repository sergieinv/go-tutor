package task

import (
	"reflect"
	"testing"
	"time"
	"unsafe"
)

// pointerPath возвращает путь к первому полю с указателями или "".
func pointerPath(t reflect.Type, path string) string {
	switch t.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.Slice, reflect.Map, reflect.Chan,
		reflect.Func, reflect.Interface, reflect.String:
		return path + " (" + t.String() + ")"
	case reflect.Array:
		if t.Len() == 0 {
			return ""
		}
		return pointerPath(t.Elem(), path+"[0]")
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			if p := pointerPath(f.Type, path+"."+f.Name); p != "" {
				return p
			}
		}
	}
	return ""
}

func TestHiddenReadingHasNoPointers(t *testing.T) {
	if p := pointerPath(reflect.TypeFor[Reading](), "Reading"); p != "" {
		t.Errorf("в Reading есть указатель: %s — GC придётся сканировать каждое показание", p)
	}
}

func TestHiddenReadingSize(t *testing.T) {
	if s := unsafe.Sizeof(Reading{}); s > 24 {
		t.Errorf("unsafe.Sizeof(Reading{}) = %d, want <= 24", s)
	}
}

func TestHiddenTimeRange(t *testing.T) {
	cases := []time.Time{
		time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1900, 3, 1, 12, 0, 0, 0, time.UTC),
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2106, 2, 8, 0, 0, 0, 0, time.UTC), // за пределами uint32-секунд
		time.Date(2200, 7, 4, 23, 59, 59, 0, time.UTC),
		time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
	}
	for _, at := range cases {
		got := NewReading(1, Pascal, 1, at, true).At()
		if !got.Equal(at) || got.Location() != time.UTC {
			t.Errorf("At() для %v = %v (%v), want тот же момент в UTC", at, got, got.Location())
		}
	}
}

func TestHiddenTimeTruncation(t *testing.T) {
	msk := time.FixedZone("MSK", 3*60*60)
	cases := []struct{ in, want time.Time }{
		{time.Date(2026, 11, 9, 13, 0, 0, 999_999_999, msk), time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)},
		{time.Date(1969, 12, 31, 23, 59, 59, 500_000_000, time.UTC), time.Date(1969, 12, 31, 23, 59, 59, 0, time.UTC)},
	}
	for _, c := range cases {
		got := NewReading(1, Celsius, 0, c.in, true).At()
		if !got.Equal(c.want) || got.Location() != time.UTC {
			t.Errorf("At() для %v = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestHiddenFieldsPreserved(t *testing.T) {
	at := time.Date(2030, 5, 6, 7, 8, 9, 0, time.UTC)
	cases := []struct {
		sensor uint32
		unit   Unit
		value  float32
		valid  bool
	}{
		{0, Celsius, 0, false},
		{4_294_967_295, Pascal, -273.15, true},
		{7, Percent, 3.4e38, false},
		{123456, Unit(200), 1e-30, true},
	}
	for _, c := range cases {
		r := NewReading(c.sensor, c.unit, c.value, at, c.valid)
		if r.Sensor() != c.sensor || r.Unit() != c.unit || r.Value() != c.value || r.Valid() != c.valid || !r.At().Equal(at) {
			t.Errorf("NewReading(%d, %d, %v, …, %v) → %d %d %v %v", c.sensor, c.unit, c.value, c.valid, r.Sensor(), r.Unit(), r.Value(), r.Valid())
		}
	}
}

var (
	sinkReading Reading
	sinkTime    time.Time
	sinkMean    float64
)

func TestHiddenNoAllocs(t *testing.T) {
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	sensor := uint32(17)
	if a := testing.AllocsPerRun(100, func() {
		sinkReading = NewReading(sensor, Celsius, 1.5, at, true)
		sinkTime = sinkReading.At()
	}); a != 0 {
		t.Errorf("NewReading + At: %v выделений, want 0", a)
	}

	l := NewLog(1000)
	r := NewReading(sensor, Celsius, 2, at, true)
	// AllocsPerRun делает один прогон для разогрева и ещё 500: всё в пределах ёмкости.
	if a := testing.AllocsPerRun(500, func() { l.Append(r) }); a != 0 {
		t.Errorf("Log.Append в пределах ёмкости: %v выделений на вызов, want 0 — журнал не должен хранить указатели", a)
	}
	if l.Len() != 501 {
		t.Fatalf("Len() = %d после 501 Append, want 501", l.Len())
	}
	if a := testing.AllocsPerRun(100, func() { sinkMean, _ = l.Mean(Celsius) }); a != 0 {
		t.Errorf("Log.Mean: %v выделений, want 0", a)
	}
}

func TestHiddenLogValues(t *testing.T) {
	at := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	l := NewLog(0) // нулевая ёмкость — Append всё равно работает
	for i := range 100 {
		l.Append(NewReading(uint32(i), Percent, float32(i), at.Add(time.Duration(i)*time.Second), i%2 == 0))
	}
	if l.Len() != 100 {
		t.Fatalf("Len() = %d, want 100", l.Len())
	}
	for _, i := range []int{0, 1, 50, 99} {
		r := l.Get(i)
		if r.Sensor() != uint32(i) || r.Value() != float32(i) || r.Valid() != (i%2 == 0) || !r.At().Equal(at.Add(time.Duration(i)*time.Second)) {
			t.Errorf("Get(%d) = sensor %d value %v valid %v at %v", i, r.Sensor(), r.Value(), r.Valid(), r.At())
		}
	}
	// Действительные — чётные 0..98, среднее 49.
	if m, ok := l.Mean(Percent); m != 49 || !ok {
		t.Errorf("Mean(Percent) = %v, %v; want 49, true", m, ok)
	}
	if m, ok := l.Mean(Pascal); m != 0 || ok {
		t.Errorf("Mean(Pascal) без показаний = %v, %v; want 0, false", m, ok)
	}
}
