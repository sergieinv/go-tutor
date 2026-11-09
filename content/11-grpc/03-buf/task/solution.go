package task

import (
	"cmp"
	"regexp"
	"slices"
	"strings"

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

var lowerSnake = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// Lint проверяет элементы, объявленные в файле fd.
func Lint(fd protoreflect.FileDescriptor) []Issue {
	var out []Issue
	add := func(rule string, el protoreflect.FullName) {
		out = append(out, Issue{Rule: rule, Element: el})
	}

	checkEnums(fd.Enums(), add)
	var walk func(protoreflect.MessageDescriptors)
	walk = func(msgs protoreflect.MessageDescriptors) {
		for i := range msgs.Len() {
			m := msgs.Get(i)
			for j := range m.Fields().Len() {
				f := m.Fields().Get(j)
				if !lowerSnake.MatchString(string(f.Name())) {
					add(RuleFieldLowerSnakeCase, f.FullName())
				}
			}
			checkEnums(m.Enums(), add)
			walk(m.Messages())
		}
	}
	walk(fd.Messages())

	for i := range fd.Services().Len() {
		sd := fd.Services().Get(i)
		for j := range sd.Methods().Len() {
			md := sd.Methods().Get(j)
			if !standard(md.Input().Name(), sd.Name(), md.Name(), "Request") {
				add(RuleRPCRequestStandardName, md.FullName())
			}
			if !standard(md.Output().Name(), sd.Name(), md.Name(), "Response") {
				add(RuleRPCResponseStandardName, md.FullName())
			}
		}
	}

	slices.SortFunc(out, func(a, b Issue) int {
		return cmp.Or(strings.Compare(string(a.Element), string(b.Element)), strings.Compare(a.Rule, b.Rule))
	})
	return out
}

func checkEnums(enums protoreflect.EnumDescriptors, add func(string, protoreflect.FullName)) {
	for i := range enums.Len() {
		v := enums.Get(i).Values().Get(0)
		if !strings.HasSuffix(string(v.Name()), "_UNSPECIFIED") {
			add(RuleEnumZeroValueSuffix, v.FullName())
		}
	}
}

// standard: GetOrderRequest или OrderServiceGetOrderRequest.
func standard(name, service, method protoreflect.Name, suffix string) bool {
	n := string(name)
	return n == string(method)+suffix || n == string(service)+string(method)+suffix
}
