package task

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	_ "google.golang.org/protobuf/types/known/emptypb"
)

// file собирает дескриптор из текстового FileDescriptorProto.
func file(t *testing.T, body string) protoreflect.FileDescriptor {
	t.Helper()
	var fdp descriptorpb.FileDescriptorProto
	src := `name: "hidden.proto" package: "hidden.v1" syntax: "proto3" ` + body
	if err := prototext.Unmarshal([]byte(src), &fdp); err != nil {
		t.Fatalf("prototext: %v", err)
	}
	fd, err := protodesc.NewFile(&fdp, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("protodesc: %v", err)
	}
	return fd
}

func expectIssues(t *testing.T, fd protoreflect.FileDescriptor, want ...Issue) {
	t.Helper()
	got := Lint(fd)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Lint =\n%v\nwant\n%v", got, want)
	}
}

func TestHiddenFieldNames(t *testing.T) {
	fd := file(t, `message_type { name: "M"
		field { name: "a" number: 1 type: TYPE_STRING label: LABEL_OPTIONAL json_name: "a" }
		field { name: "a1" number: 2 type: TYPE_STRING label: LABEL_OPTIONAL }
		field { name: "address_line_1" number: 3 type: TYPE_STRING label: LABEL_OPTIONAL }
		field { name: "_a" number: 4 type: TYPE_STRING label: LABEL_OPTIONAL }
		field { name: "a__b" number: 5 type: TYPE_STRING label: LABEL_OPTIONAL }
		field { name: "b_" number: 6 type: TYPE_STRING label: LABEL_OPTIONAL }
		field { name: "C" number: 7 type: TYPE_STRING label: LABEL_OPTIONAL }
		field { name: "ab_cD" number: 8 type: TYPE_STRING label: LABEL_OPTIONAL }
		field { name: "x9_y" number: 9 type: TYPE_STRING label: LABEL_OPTIONAL }
		field { name: "line_2b" number: 10 type: TYPE_STRING label: LABEL_OPTIONAL } }`)
	expectIssues(t, fd,
		Issue{RuleFieldLowerSnakeCase, "hidden.v1.M.C"},
		Issue{RuleFieldLowerSnakeCase, "hidden.v1.M._a"},
		Issue{RuleFieldLowerSnakeCase, "hidden.v1.M.a__b"},
		Issue{RuleFieldLowerSnakeCase, "hidden.v1.M.ab_cD"},
		Issue{RuleFieldLowerSnakeCase, "hidden.v1.M.b_"},
	)
}

func TestHiddenDeepNesting(t *testing.T) {
	fd := file(t, `message_type { name: "A"
		nested_type { name: "B"
			nested_type { name: "C"
				field { name: "badName" number: 1 type: TYPE_STRING label: LABEL_OPTIONAL }
				enum_type { name: "Mode" value { name: "MODE_NONE" number: 0 } } } } }
		enum_type { name: "Top" value { name: "UNSPECIFIED" number: 0 } value { name: "TOP_X_UNSPECIFIED" number: 1 } }
		enum_type { name: "Good" value { name: "GOOD_UNSPECIFIED" number: 0 } }`)
	expectIssues(t, fd,
		Issue{RuleEnumZeroValueSuffix, "hidden.v1.A.B.C.MODE_NONE"},
		Issue{RuleFieldLowerSnakeCase, "hidden.v1.A.B.C.badName"},
		Issue{RuleEnumZeroValueSuffix, "hidden.v1.UNSPECIFIED"},
	)
}

func TestHiddenServices(t *testing.T) {
	fd := file(t, `dependency: "google/protobuf/empty.proto"
		message_type { name: "PingRequest" }
		message_type { name: "PingResponse" }
		message_type { name: "HealthServicePingRequest" }
		message_type { name: "HealthServicePingResponse" }
		message_type { name: "PongRequest" }
		service { name: "PingService"
			method { name: "Ping" input_type: ".hidden.v1.PingRequest" output_type: ".hidden.v1.PingResponse" }
			method { name: "Pong" input_type: ".hidden.v1.PingRequest" output_type: ".google.protobuf.Empty" } }
		service { name: "HealthService"
			method { name: "Ping" input_type: ".hidden.v1.HealthServicePingRequest" output_type: ".hidden.v1.PingResponse" }
			method { name: "Check" input_type: ".hidden.v1.PingServiceCheckRequest" output_type: ".hidden.v1.HealthServicePingResponse" } }
		message_type { name: "PingServiceCheckRequest" }`)
	expectIssues(t, fd,
		Issue{RuleRPCRequestStandardName, "hidden.v1.HealthService.Check"},
		Issue{RuleRPCResponseStandardName, "hidden.v1.HealthService.Check"},
		Issue{RuleRPCRequestStandardName, "hidden.v1.PingService.Pong"},
		Issue{RuleRPCResponseStandardName, "hidden.v1.PingService.Pong"},
	)
}

func TestHiddenImportsNotChecked(t *testing.T) {
	// Импортированный файл (empty.proto) не проверяется — только объявленное здесь.
	fd := file(t, `dependency: "google/protobuf/empty.proto"
		message_type { name: "Ok" field { name: "value" number: 1 type: TYPE_MESSAGE label: LABEL_OPTIONAL type_name: ".google.protobuf.Empty" } }`)
	expectIssues(t, fd)
}

func TestHiddenSortSameElement(t *testing.T) {
	fd := file(t, `message_type { name: "X" }
		service { name: "S" method { name: "Do" input_type: ".hidden.v1.X" output_type: ".hidden.v1.X" } }
		message_type { name: "Z" field { name: "Bad" number: 1 type: TYPE_STRING label: LABEL_OPTIONAL } }
		message_type { name: "B" field { name: "Bad" number: 1 type: TYPE_STRING label: LABEL_OPTIONAL } }`)
	expectIssues(t, fd,
		Issue{RuleFieldLowerSnakeCase, "hidden.v1.B.Bad"},
		Issue{RuleRPCRequestStandardName, "hidden.v1.S.Do"},
		Issue{RuleRPCResponseStandardName, "hidden.v1.S.Do"},
		Issue{RuleFieldLowerSnakeCase, "hidden.v1.Z.Bad"},
	)
}
