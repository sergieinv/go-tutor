package task

import (
	"errors"
	"fmt"

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

// entry — вхождение поля вместе со значением (для Varint).
type entry struct {
	Field
	varint uint64
}

// scan проходит сообщение целиком и возвращает все вхождения полей.
func scan(b []byte) ([]entry, error) {
	var out []entry
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, fmt.Errorf("%w: tag: %v", ErrMalformed, protowire.ParseError(n))
		}
		b = b[n:]
		e := entry{Field: Field{Num: num, Type: typ}}
		switch typ {
		case protowire.VarintType:
			e.varint, n = protowire.ConsumeVarint(b)
		case protowire.Fixed32Type:
			_, n = protowire.ConsumeFixed32(b)
		case protowire.Fixed64Type:
			_, n = protowire.ConsumeFixed64(b)
		case protowire.BytesType:
			_, n = protowire.ConsumeBytes(b)
		default:
			// Группы (3, 4) устарели, 6 и 7 не определены.
			return nil, fmt.Errorf("%w: field %d: wire type %d", ErrMalformed, num, typ)
		}
		if n < 0 {
			return nil, fmt.Errorf("%w: field %d: %v", ErrMalformed, num, protowire.ParseError(n))
		}
		b = b[n:]
		out = append(out, e)
	}
	return out, nil
}

// Fields возвращает поля верхнего уровня в порядке появления.
func Fields(b []byte) ([]Field, error) {
	entries, err := scan(b)
	if err != nil {
		return nil, err
	}
	out := make([]Field, len(entries))
	for i, e := range entries {
		out[i] = e.Field
	}
	return out, nil
}

// Varint возвращает значение varint-поля num: при повторах побеждает
// последнее вхождение, как при обычном разборе.
func Varint(b []byte, num protowire.Number) (uint64, bool, error) {
	entries, err := scan(b)
	if err != nil {
		return 0, false, err
	}
	var v uint64
	ok := false
	for _, e := range entries {
		if e.Num != num {
			continue
		}
		if e.Type != protowire.VarintType {
			return 0, false, fmt.Errorf("%w: field %d: wire type %d, want varint", ErrWireType, num, e.Type)
		}
		v, ok = e.varint, true
	}
	return v, ok, nil
}
