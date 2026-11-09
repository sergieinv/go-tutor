package task

import (
	"testing"
	"time"
	"unsafe"
)

func TestReadingRoundTrip(t *testing.T) {
	at := time.Date(2026, 11, 9, 10, 30, 15, 0, time.UTC)
	r := NewReading(42, Celsius, 21.5, at, true)
	if r.Sensor() != 42 || r.Unit() != Celsius || r.Value() != 21.5 || !r.Valid() || !r.At().Equal(at) {
		t.Fatalf("NewReading(42, Celsius, 21.5, %v, true) → %d %d %v %v %v", at, r.Sensor(), r.Unit(), r.Value(), r.At(), r.Valid())
	}
}

func TestReadingSize(t *testing.T) {
	if s := unsafe.Sizeof(Reading{}); s > 24 {
		t.Errorf("unsafe.Sizeof(Reading{}) = %d, want <= 24", s)
	}
}

func TestLogMean(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := NewLog(4)
	l.Append(NewReading(1, Celsius, 20, at, true))
	l.Append(NewReading(2, Celsius, 100, at, false)) // недействительное — не считается
	l.Append(NewReading(3, Celsius, 22, at, true))
	l.Append(NewReading(4, Percent, 55, at, true))
	if m, ok := l.Mean(Celsius); m != 21 || !ok {
		t.Errorf("Mean(Celsius) = %v, %v; want 21, true", m, ok)
	}
	if l.Len() != 4 || l.Get(3).Sensor() != 4 {
		t.Errorf("Len() = %d, Get(3).Sensor() = %d; want 4, 4", l.Len(), l.Get(3).Sensor())
	}
}
