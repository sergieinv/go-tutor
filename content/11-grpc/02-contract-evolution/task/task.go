package task

import (
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Правила совместимости — имена по образцу buf breaking.
const (
	RuleNoDelete        = "FIELD_NO_DELETE_UNLESS_NUMBER_RESERVED"
	RuleSameCardinality = "FIELD_SAME_CARDINALITY"
	RuleSameType        = "FIELD_SAME_TYPE"
	RuleSameOneof       = "FIELD_SAME_ONEOF"
	RuleSameJSONName    = "FIELD_SAME_JSON_NAME"
	RuleReservedReused  = "RESERVED_REUSED"
)

// Violation — нарушение правила для поля с номером Number.
type Violation struct {
	Rule   string
	Number protoreflect.FieldNumber
}

// Check сравнивает прежнюю версию сообщения prev с новой next.
func Check(prev, next protoreflect.MessageDescriptor) []Violation {
	// TODO: пройдите поля prev по номерам, найдите их в next,
	// затем проверьте поля next против reserved из prev.
	return nil
}
