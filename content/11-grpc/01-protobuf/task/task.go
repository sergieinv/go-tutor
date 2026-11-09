package task

import (
	"errors"

	"google.golang.org/protobuf/encoding/protowire"
)

var (
	// ErrMalformed — байты не разбираются как сообщение protobuf.
	ErrMalformed = errors.New("protobuf: malformed message")
	// ErrWireType — поле пришло не с тем wire type, которого ждали.
	ErrWireType = errors.New("protobuf: unexpected wire type")
)

// Field — одно вхождение поля верхнего уровня.
type Field struct {
	Num  protowire.Number
	Type protowire.Type
}

// Fields возвращает поля верхнего уровня в порядке появления.
func Fields(b []byte) ([]Field, error) {
	// TODO: пройдите b с помощью protowire.ConsumeTag и Consume* по wire type.
	return nil, nil
}

// Varint возвращает значение varint-поля num: при повторах побеждает
// последнее вхождение, как при обычном разборе.
func Varint(b []byte, num protowire.Number) (uint64, bool, error) {
	// TODO: проверьте всё сообщение, затем найдите последнее вхождение num.
	return 0, false, nil
}
