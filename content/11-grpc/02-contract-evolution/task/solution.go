package task

import (
	"cmp"
	"slices"

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
	var out []Violation
	add := func(rule string, n protoreflect.FieldNumber) {
		out = append(out, Violation{Rule: rule, Number: n})
	}

	pf := prev.Fields()
	for i := range pf.Len() {
		old := pf.Get(i)
		cur := next.Fields().ByNumber(old.Number())
		if cur == nil {
			if !next.ReservedRanges().Has(old.Number()) {
				add(RuleNoDelete, old.Number())
			}
			continue
		}
		if cardinality(old) != cardinality(cur) {
			add(RuleSameCardinality, old.Number())
			continue
		}
		if !sameType(old, cur) {
			add(RuleSameType, old.Number())
		}
		if oneofName(old) != oneofName(cur) {
			add(RuleSameOneof, old.Number())
		}
		if old.JSONName() != cur.JSONName() {
			add(RuleSameJSONName, old.Number())
		}
	}

	nf := next.Fields()
	for i := range nf.Len() {
		f := nf.Get(i)
		if prev.ReservedRanges().Has(f.Number()) || prev.ReservedNames().Has(f.Name()) {
			add(RuleReservedReused, f.Number())
		}
	}

	slices.SortFunc(out, func(a, b Violation) int {
		return cmp.Or(cmp.Compare(a.Number, b.Number), cmp.Compare(a.Rule, b.Rule))
	})
	return out
}

// cardinality — вид поля: одиночное, список или map.
func cardinality(f protoreflect.FieldDescriptor) string {
	switch {
	case f.IsMap():
		return "map"
	case f.IsList():
		return "list"
	default:
		return "single"
	}
}

// sameType сравнивает типы значений; у map — ключ и значение.
func sameType(a, b protoreflect.FieldDescriptor) bool {
	if a.IsMap() {
		return sameType(a.MapKey(), b.MapKey()) && sameType(a.MapValue(), b.MapValue())
	}
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return a.Message().FullName() == b.Message().FullName()
	case protoreflect.EnumKind:
		return a.Enum().FullName() == b.Enum().FullName()
	}
	return true
}

// oneofName — имя настоящего oneof; синтетический oneof поля optional
// не считается.
func oneofName(f protoreflect.FieldDescriptor) protoreflect.Name {
	od := f.ContainingOneof()
	if od == nil || od.IsSynthetic() {
		return ""
	}
	return od.Name()
}
