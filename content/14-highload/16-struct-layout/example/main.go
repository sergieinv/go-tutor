package main

import (
	"fmt"
	"testing"
	"time"
	"unsafe"
)

// Поля лежат в памяти в порядке объявления, и каждое начинается
// со смещения, кратного своему выравниванию: `int64` — с 8,
// `int32` — с 4, `bool` — с 1. Между полями компилятор вставляет
// пустые байты (padding), а в конце добивает размер до кратного
// выравниванию всей структуры, чтобы в массиве следующий элемент
// тоже был выровнен.
type orderBad struct {
	paid     bool  // 1 байт + 7 пустых
	id       int64 // 8
	urgent   bool  // 1 байт + 3 пустых
	qty      int32 // 4
	gift     bool  // 1 байт + 7 пустых
	amount   int64 // 8
	discount bool  // 1 байт + 7 пустых в хвосте: итого 48
}

// Те же поля от больших к меньшим: дыр нет, только хвост.
type orderGood struct {
	id       int64
	amount   int64
	qty      int32
	paid     bool
	urgent   bool
	gift     bool
	discount bool
}

// Поле нулевого размера в конце структуры получает свой байт и
// выравнивание: иначе указатель на него смотрел бы за конец объекта.
type withTail struct {
	n    int64
	done struct{}
}

type withHead struct {
	done struct{}
	n    int64
}

// `time.Time` — 24 байта и указатель на `*time.Location` внутри.
// Если пояс не нужен, момент хранят как `int64` (Unix-время).
type eventTime struct {
	at time.Time
}

type eventUnix struct {
	at int64
}

var (
	sinkValues   []orderGood
	sinkPointers []*orderGood
)

func main() {
	fmt.Println("orderBad:", unsafe.Sizeof(orderBad{}), "байт, orderGood:", unsafe.Sizeof(orderGood{}), "байт")
	var b orderBad
	fmt.Println("смещения в orderBad: id", unsafe.Offsetof(b.id), "qty", unsafe.Offsetof(b.qty), "amount", unsafe.Offsetof(b.amount))
	fmt.Println("выравнивание: int64", unsafe.Alignof(b.id), "int32", unsafe.Alignof(b.qty), "bool", unsafe.Alignof(b.paid), "orderBad", unsafe.Alignof(b))
	fmt.Println("struct{} в конце:", unsafe.Sizeof(withTail{}), "в начале:", unsafe.Sizeof(withHead{}))
	fmt.Println("time.Time:", unsafe.Sizeof(eventTime{}), "int64:", unsafe.Sizeof(eventUnix{}))

	// Срез структур — один непрерывный массив, одно выделение.
	// Срез указателей — массив указателей плюс объект на каждый
	// элемент, разбросанный по куче.
	const n = 1000
	values := testing.AllocsPerRun(10, func() {
		s := make([]orderGood, n)
		for i := range s {
			s[i].id = int64(i)
		}
		sinkValues = s
	})
	pointers := testing.AllocsPerRun(10, func() {
		s := make([]*orderGood, n)
		for i := range s {
			s[i] = &orderGood{id: int64(i)}
		}
		sinkPointers = s
	})
	fmt.Println("выделений на 1000 заказов: []orderGood", values, "[]*orderGood", pointers)

	// Сумма по одному полю читает всю структуру целиком: соседние
	// поля едут в кеш вместе с нужным. В раскладке «структура
	// массивов» (SoA) поле `amount` лежит отдельным плотным срезом.
	fmt.Println("байт пройдено ради суммы amount: AoS", n*unsafe.Sizeof(orderBad{}), "SoA", n*unsafe.Sizeof(int64(0)))
}
