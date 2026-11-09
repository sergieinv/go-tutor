package task

// Kind — вид значения в Value.
type Kind int

const (
	KindAny Kind = iota
	KindInt64
	KindBool
	KindString
)

// Value хранит число, флаг, строку или произвольное значение.
//
// Простейший вариант — одно поле any — работает, но упаковка
// int64 и string в интерфейс выделяет память. Выберите поля так,
// чтобы скаляры обходились без аллокаций.
type Value struct {
	v any
}

func Int64Value(v int64) Value {
	// TODO: без упаковки в any
	return Value{}
}

func BoolValue(v bool) Value {
	// TODO
	return Value{}
}

func StringValue(v string) Value {
	// TODO: без упаковки в any
	return Value{}
}

func AnyValue(v any) Value {
	// TODO
	return Value{}
}

func (v Value) Kind() Kind {
	// TODO
	return KindAny
}

// Int64 паникует, если вид не KindInt64.
func (v Value) Int64() int64 {
	// TODO
	return 0
}

// Bool паникует, если вид не KindBool.
func (v Value) Bool() bool {
	// TODO
	return false
}

// Str паникует, если вид не KindString.
func (v Value) Str() string {
	// TODO
	return ""
}

// Any возвращает значение любого вида как any.
func (v Value) Any() any {
	// TODO
	return v.v
}

// Equal сравнивает значения и никогда не паникует.
func (v Value) Equal(o Value) bool {
	// TODO: разные виды, скаляры, KindAny с несравнимыми типами
	return false
}
