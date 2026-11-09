package task

import "reflect"

// Kind — вид значения в Value.
type Kind int

const (
	KindAny Kind = iota
	KindInt64
	KindBool
	KindString
)

// Value хранит число, флаг, строку или произвольное значение.
// Скаляры лежат в своих полях, а не в any: упаковка int64 или
// string в интерфейс выделила бы память, как только Value
// уйдёт в кучу.
type Value struct {
	kind Kind
	num  uint64 // int64 и bool
	str  string
	any  any // только для KindAny
}

func Int64Value(v int64) Value { return Value{kind: KindInt64, num: uint64(v)} }

func BoolValue(v bool) Value {
	var n uint64
	if v {
		n = 1
	}
	return Value{kind: KindBool, num: n}
}

func StringValue(v string) Value { return Value{kind: KindString, str: v} }

func AnyValue(v any) Value { return Value{kind: KindAny, any: v} }

func (v Value) Kind() Kind { return v.kind }

func (v Value) Int64() int64 {
	v.must(KindInt64)
	return int64(v.num)
}

func (v Value) Bool() bool {
	v.must(KindBool)
	return v.num == 1
}

func (v Value) Str() string {
	v.must(KindString)
	return v.str
}

// Any возвращает значение как any — здесь упаковка неизбежна.
func (v Value) Any() any {
	switch v.kind {
	case KindInt64:
		return int64(v.num)
	case KindBool:
		return v.num == 1
	case KindString:
		return v.str
	}
	return v.any
}

func (v Value) must(k Kind) {
	if v.kind != k {
		panic("task.Value: wrong kind")
	}
}

// Equal сравнивает значения и никогда не паникует.
func (v Value) Equal(o Value) bool {
	if v.kind != o.kind {
		return false
	}
	switch v.kind {
	case KindInt64, KindBool:
		return v.num == o.num
	case KindString:
		return v.str == o.str
	}
	return equalAny(v.any, o.any)
}

func equalAny(a, b any) (eq bool) {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	// Несравнимый динамический тип: == паниковало бы.
	if !reflect.TypeOf(a).Comparable() || !reflect.TypeOf(b).Comparable() {
		return false
	}
	// Страховка: сравнимая структура может содержать поле-интерфейс
	// с несравнимым значением внутри.
	defer func() {
		if recover() != nil {
			eq = false
		}
	}()
	return a == b
}
