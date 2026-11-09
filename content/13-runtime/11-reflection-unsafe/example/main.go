// `reflect` даёт доступ к типам и значениям во время выполнения
// по тем же дескрипторам типов, что лежат в интерфейсах, а `unsafe`
// снимает проверки системы типов. Первое медленно, второе опасно:
// пример показывает, как пользоваться обоими там, где это оправдано.

package main

import (
	"fmt"
	"math"
	"reflect"
	"time"
	"unsafe"
)

type Config struct {
	Host    string        `env:"HOST"`
	Port    int           `env:"PORT,required"`
	Timeout time.Duration `env:"TIMEOUT"`
	secret  string        // неэкспортируемое: reflect не даст его изменить
}

func (c Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

func main() {
	// `reflect.TypeFor[T]()` (Go 1.22) — тип без значения.
	// Kind — «форма» типа, Type — сам тип: у `time.Duration`
	// форма int64, но это другой тип со своими методами.
	t := reflect.TypeFor[Config]()
	fmt.Println("type:", t, "kind:", t.Kind(), "fields:", t.NumField())
	d := reflect.TypeFor[time.Duration]()
	fmt.Println("duration:", d, "kind:", d.Kind())

	// Поля и теги — из дескриптора типа, без значения.
	// `Tag.Lookup` отличает пустой тег от отсутствующего.
	for i := range t.NumField() {
		f := t.Field(i)
		tag, ok := f.Tag.Lookup("env")
		fmt.Printf("  %-7s %-13v exported=%-5v env=%q %v\n", f.Name, f.Type, f.IsExported(), tag, ok)
	}

	// `reflect.ValueOf(cfg)` получает копию — менять её бессмысленно,
	// поэтому значение не адресуемо и `CanSet` = false. Через указатель
	// и `Elem()` значение адресуемо, а экспортируемые поля можно менять.
	cfg := Config{Host: "localhost"}
	fmt.Println("copy settable:", reflect.ValueOf(cfg).Field(1).CanSet())
	v := reflect.ValueOf(&cfg).Elem()
	fmt.Println("via pointer settable:", v.Field(1).CanSet(), "unexported:", v.Field(3).CanSet())
	v.FieldByName("Port").SetInt(8080)
	v.FieldByName("Timeout").Set(reflect.ValueOf(3 * time.Second))
	fmt.Println("after reflect set:", cfg.Port, cfg.Timeout)

	// Вызов метода по имени: аргументы и результаты — срезы Value.
	// Каждый такой вызов упаковывает значения и выделяет память.
	out := v.MethodByName("Addr").Call(nil)
	fmt.Println("Addr via reflect:", out[0].String())

	// `unsafe.Sizeof/Alignof/Offsetof` вычисляются компилятором
	// и безопасны. Поля выравниваются, порядок полей меняет размер.
	type padded struct {
		A bool
		B int64
		C bool
	}
	type packed struct {
		B    int64
		A, C bool
	}
	var p padded
	fmt.Println("padded:", unsafe.Sizeof(p), "offset B:", unsafe.Offsetof(p.B), "align:", unsafe.Alignof(p))
	fmt.Println("packed:", unsafe.Sizeof(packed{}))

	// Шаблон 1 из `go doc unsafe.Pointer`: *T1 → Pointer → *T2 при
	// одинаковой раскладке. Так устроен `math.Float64bits`.
	f := 1.5
	bits := *(*uint64)(unsafe.Pointer(&f))
	fmt.Println("float bits equal:", bits == math.Float64bits(f))

	// `unsafe.String` строит строку над байтами без копии.
	// Оправдано, только если байты больше никогда не меняются:
	// здесь срез локальный и после конвертации не используется.
	raw := []byte("read-only payload")
	s := unsafe.String(unsafe.SliceData(raw), len(raw))
	fmt.Println("zero-copy string:", s, unsafe.StringData(s) == &raw[0])

	// `unsafe.Slice` — срез над указателем и длиной: например,
	// байтовое представление массива чисел без копирования.
	nums := [2]uint16{0x0102, 0x0304}
	view := unsafe.Slice((*byte)(unsafe.Pointer(&nums[0])), len(nums)*2)
	fmt.Println("bytes of [2]uint16:", len(view))
}
