package task

import (
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Правила — имена как у buf lint.
const (
	RuleFieldLowerSnakeCase     = "FIELD_LOWER_SNAKE_CASE"
	RuleEnumZeroValueSuffix     = "ENUM_ZERO_VALUE_SUFFIX"
	RuleRPCRequestStandardName  = "RPC_REQUEST_STANDARD_NAME"
	RuleRPCResponseStandardName = "RPC_RESPONSE_STANDARD_NAME"
)

// Issue — нарушение правила Rule элементом Element.
type Issue struct {
	Rule    string
	Element protoreflect.FullName
}

// Lint проверяет элементы, объявленные в файле fd.
func Lint(fd protoreflect.FileDescriptor) []Issue {
	// TODO: обойдите сообщения (с вложенными), enum и методы сервисов.
	return nil
}
