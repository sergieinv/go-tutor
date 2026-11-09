package task

import "time"

// Unit — единица измерения показания.
type Unit uint8

const (
	Celsius Unit = iota + 1
	Percent
	Pascal
)

// Reading — 24 байта без указателей: момент — Unix-секунды,
// поля от больших к меньшим, однобайтовые — в хвосте.
type Reading struct {
	at     int64   // 8: Unix-время в секундах
	sensor uint32  // 4
	value  float32 // 4
	unit   Unit    // 1
	valid  bool    // 1 + 6 байт хвоста
}

func NewReading(sensor uint32, unit Unit, value float32, at time.Time, valid bool) Reading {
	return Reading{at: at.Unix(), sensor: sensor, value: value, unit: unit, valid: valid}
}

func (r Reading) Sensor() uint32 { return r.sensor }
func (r Reading) Unit() Unit     { return r.unit }
func (r Reading) Value() float32 { return r.value }
func (r Reading) Valid() bool    { return r.valid }
func (r Reading) At() time.Time  { return time.Unix(r.at, 0).UTC() }

// Log хранит значения подряд: Append в пределах ёмкости не выделяет память.
type Log struct {
	items []Reading
}

func NewLog(capacity int) *Log {
	return &Log{items: make([]Reading, 0, capacity)}
}

func (l *Log) Append(r Reading)  { l.items = append(l.items, r) }
func (l *Log) Len() int          { return len(l.items) }
func (l *Log) Get(i int) Reading { return l.items[i] }

func (l *Log) Mean(unit Unit) (float64, bool) {
	var sum float64
	n := 0
	for i := range l.items {
		r := &l.items[i] // без копии 24 байт на итерацию
		if r.valid && r.unit == unit {
			sum += float64(r.value)
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}
