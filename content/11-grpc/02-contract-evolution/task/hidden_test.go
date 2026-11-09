package task

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// pair собирает файл из текстового FileDescriptorProto и возвращает
// сообщения Prev и Next.
func pair(t *testing.T, body string) (protoreflect.MessageDescriptor, protoreflect.MessageDescriptor) {
	t.Helper()
	var fdp descriptorpb.FileDescriptorProto
	src := `name: "hidden.proto" package: "hidden" syntax: "proto3" ` + body
	if err := prototext.Unmarshal([]byte(src), &fdp); err != nil {
		t.Fatalf("prototext: %v", err)
	}
	fd, err := protodesc.NewFile(&fdp, nil)
	if err != nil {
		t.Fatalf("protodesc: %v", err)
	}
	return fd.Messages().ByName("Prev"), fd.Messages().ByName("Next")
}

func expect(t *testing.T, prev, next protoreflect.MessageDescriptor, want ...Violation) {
	t.Helper()
	got := Check(prev, next)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Check =\n%v\nwant\n%v", got, want)
	}
}

func TestHiddenOptionalIsNotOneof(t *testing.T) {
	// optional порождает синтетический oneof "_qty" — это не oneof.
	prev, next := pair(t, `
		message_type { name: "Prev" field { name: "qty" number: 1 label: LABEL_OPTIONAL type: TYPE_INT64 } }
		message_type { name: "Next"
			field { name: "qty" number: 1 label: LABEL_OPTIONAL type: TYPE_INT64 oneof_index: 0 proto3_optional: true }
			oneof_decl { name: "_qty" } }`)
	expect(t, prev, next)
	expect(t, next, prev)
}

func TestHiddenReferencedTypes(t *testing.T) {
	prev, next := pair(t, `
		message_type { name: "ItemA" }
		message_type { name: "ItemB" }
		enum_type { name: "StatusA" value { name: "STATUS_A_UNSPECIFIED" number: 0 } }
		enum_type { name: "StatusB" value { name: "STATUS_B_UNSPECIFIED" number: 0 } }
		message_type { name: "Prev"
			field { name: "item" number: 1 label: LABEL_OPTIONAL type: TYPE_MESSAGE type_name: ".hidden.ItemA" }
			field { name: "status" number: 2 label: LABEL_OPTIONAL type: TYPE_ENUM type_name: ".hidden.StatusA" }
			field { name: "same" number: 3 label: LABEL_OPTIONAL type: TYPE_MESSAGE type_name: ".hidden.ItemA" }
			field { name: "qty" number: 4 label: LABEL_OPTIONAL type: TYPE_INT32 } }
		message_type { name: "Next"
			field { name: "item" number: 1 label: LABEL_OPTIONAL type: TYPE_MESSAGE type_name: ".hidden.ItemB" }
			field { name: "status" number: 2 label: LABEL_OPTIONAL type: TYPE_ENUM type_name: ".hidden.StatusB" }
			field { name: "same" number: 3 label: LABEL_OPTIONAL type: TYPE_MESSAGE type_name: ".hidden.ItemA" }
			field { name: "qty" number: 4 label: LABEL_OPTIONAL type: TYPE_INT64 } }`)
	expect(t, prev, next,
		Violation{RuleSameType, 1}, Violation{RuleSameType, 2}, Violation{RuleSameType, 4})
}

func TestHiddenOneofChanges(t *testing.T) {
	prev, next := pair(t, `
		message_type { name: "Prev"
			field { name: "card" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING oneof_index: 0 }
			field { name: "cash" number: 2 label: LABEL_OPTIONAL type: TYPE_BOOL oneof_index: 0 }
			field { name: "email" number: 3 label: LABEL_OPTIONAL type: TYPE_STRING oneof_index: 1 }
			field { name: "free" number: 4 label: LABEL_OPTIONAL type: TYPE_STRING }
			oneof_decl { name: "pay" }
			oneof_decl { name: "contact" } }
		message_type { name: "Next"
			field { name: "card" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING oneof_index: 0 }
			field { name: "cash" number: 2 label: LABEL_OPTIONAL type: TYPE_BOOL oneof_index: 0 }
			field { name: "email" number: 3 label: LABEL_OPTIONAL type: TYPE_STRING oneof_index: 0 }
			field { name: "free" number: 4 label: LABEL_OPTIONAL type: TYPE_STRING oneof_index: 1 }
			oneof_decl { name: "payment" }
			oneof_decl { name: "extra" } }`)
	expect(t, prev, next,
		Violation{RuleSameOneof, 1}, Violation{RuleSameOneof, 2},
		Violation{RuleSameOneof, 3}, Violation{RuleSameOneof, 4})
}

func TestHiddenCardinalityOnly(t *testing.T) {
	// Если вид поля сменился, остальные правила для номера не проверяются.
	prev, next := pair(t, `
		message_type { name: "Prev"
			field { name: "tags" number: 1 label: LABEL_REPEATED type: TYPE_STRING }
			field { name: "attrs" number: 2 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".hidden.Prev.AttrsEntry" }
			field { name: "ids" number: 3 label: LABEL_REPEATED type: TYPE_INT64 }
			nested_type { name: "AttrsEntry" options { map_entry: true }
				field { name: "key" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING }
				field { name: "value" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING } } }
		message_type { name: "Next"
			field { name: "tag_count" number: 1 label: LABEL_OPTIONAL type: TYPE_INT64 }
			field { name: "attrs" number: 2 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".hidden.Attr" }
			field { name: "ids" number: 3 label: LABEL_OPTIONAL type: TYPE_INT64 } }
		message_type { name: "Attr" }`)
	expect(t, prev, next,
		Violation{RuleSameCardinality, 1}, Violation{RuleSameCardinality, 2}, Violation{RuleSameCardinality, 3})
}

func TestHiddenMapValueType(t *testing.T) {
	prev, next := pair(t, `
		message_type { name: "Prev"
			field { name: "m" number: 1 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".hidden.Prev.MEntry" }
			field { name: "k" number: 2 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".hidden.Prev.KEntry" }
			nested_type { name: "MEntry" options { map_entry: true }
				field { name: "key" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING }
				field { name: "value" number: 2 label: LABEL_OPTIONAL type: TYPE_INT32 } }
			nested_type { name: "KEntry" options { map_entry: true }
				field { name: "key" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING }
				field { name: "value" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING } } }
		message_type { name: "Next"
			field { name: "m" number: 1 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".hidden.Next.MEntry" }
			field { name: "k" number: 2 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".hidden.Next.KEntry" }
			nested_type { name: "MEntry" options { map_entry: true }
				field { name: "key" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING }
				field { name: "value" number: 2 label: LABEL_OPTIONAL type: TYPE_INT32 } }
			nested_type { name: "KEntry" options { map_entry: true }
				field { name: "key" number: 1 label: LABEL_OPTIONAL type: TYPE_INT64 }
				field { name: "value" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING } } }`)
	expect(t, prev, next, Violation{RuleSameType, 2})
}

func TestHiddenReservedRanges(t *testing.T) {
	// reserved 20 to 30 — в дескрипторе end не включается: 20..31.
	prev, next := pair(t, `
		message_type { name: "Prev"
			field { name: "a" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING }
			field { name: "b" number: 22 label: LABEL_OPTIONAL type: TYPE_STRING }
			field { name: "c" number: 40 label: LABEL_OPTIONAL type: TYPE_STRING }
			reserved_range { start: 50 end: 61 }
			reserved_name: "coupon" }
		message_type { name: "Next"
			field { name: "a" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING }
			field { name: "late" number: 31 label: LABEL_OPTIONAL type: TYPE_STRING }
			field { name: "in_range" number: 55 label: LABEL_OPTIONAL type: TYPE_STRING }
			field { name: "coupon" number: 60 label: LABEL_OPTIONAL type: TYPE_STRING }
			field { name: "edge" number: 61 label: LABEL_OPTIONAL type: TYPE_STRING }
			reserved_range { start: 20 end: 31 } }`)
	expect(t, prev, next,
		Violation{RuleNoDelete, 40}, Violation{RuleReservedReused, 55}, Violation{RuleReservedReused, 60})
}

func TestHiddenJSONNameAndSort(t *testing.T) {
	// Переименование с json_name, равным старому имени, JSON не ломает.
	prev, next := pair(t, `
		message_type { name: "Prev"
			field { name: "customer" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING }
			field { name: "phone" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING } }
		message_type { name: "Next"
			field { name: "customer_email" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "customer" }
			field { name: "phone_number" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING oneof_index: 0 }
			oneof_decl { name: "contact" } }`)
	expect(t, prev, next, Violation{RuleSameJSONName, 1}, Violation{RuleSameOneof, 1})
}
