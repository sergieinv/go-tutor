package task

import "time"

// Unit — единица измерения показания.
type Unit uint8

const (
	Celsius Unit = iota + 1
	Percent
	Pascal
)

// Reading — показание датчика. Таких в памяти миллионы, и сейчас
// каждое занимает 48 байт, а из-за time.Time внутри есть указатель.
// Перестройте поля: Sizeof(Reading{}) <= 24 и ни одного указателя.
// Методы ниже должны вести себя как прежде.
type Reading struct {
	valid  bool
	at     time.Time
	unit   Unit
	sensor uint32
	value  float32
}

// NewReading создаёт показание; момент хранится с точностью до секунды.
func NewReading(sensor uint32, unit Unit, value float32, at time.Time, valid bool) Reading {
	return Reading{
		valid:  valid,
		at:     time.Unix(at.Unix(), 0).UTC(),
		unit:   unit,
		sensor: sensor,
		value:  value,
	}
}

func (r Reading) Sensor() uint32 { return r.sensor }
func (r Reading) Unit() Unit     { return r.unit }
func (r Reading) Value() float32 { return r.value }
func (r Reading) Valid() bool    { return r.valid }

// At возвращает момент показания в UTC, усечённый до секунды.
func (r Reading) At() time.Time { return r.at }

// Log — журнал показаний. Сейчас он хранит указатели, и каждое
// Append выделяет память под отдельный объект.
type Log struct {
	items []*Reading
}

// NewLog создаёт журнал с заранее выделенным местом под capacity показаний.
func NewLog(capacity int) *Log {
	return &Log{items: make([]*Reading, 0, capacity)}
}

func (l *Log) Append(r Reading) { l.items = append(l.items, &r) }
func (l *Log) Len() int         { return len(l.items) }
func (l *Log) Get(i int) Reading {
	return *l.items[i]
}

// Mean возвращает среднее значение действительных (Valid) показаний
// в единице unit и true; если таких нет — 0 и false.
func (l *Log) Mean(unit Unit) (float64, bool) {
	var sum float64
	n := 0
	for _, r := range l.items {
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
