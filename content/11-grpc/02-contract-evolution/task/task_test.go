package task

import (
	"slices"
	"testing"
)

func TestCompatible(t *testing.T) {
	prev := (&OrderV1{}).ProtoReflect().Descriptor()
	next := (&OrderV2Compatible{}).ProtoReflect().Descriptor()
	if got := Check(prev, next); len(got) != 0 {
		t.Fatalf("Check(V1, V2Compatible) = %v, want пусто", got)
	}
}

func TestSameMessage(t *testing.T) {
	d := (&OrderV1{}).ProtoReflect().Descriptor()
	if got := Check(d, d); len(got) != 0 {
		t.Fatalf("Check(V1, V1) = %v, want пусто", got)
	}
}

func TestBroken(t *testing.T) {
	prev := (&OrderV1{}).ProtoReflect().Descriptor()
	next := (&OrderV2Broken{}).ProtoReflect().Descriptor()
	want := []Violation{
		{RuleSameJSONName, 2},
		{RuleNoDelete, 3},
		{RuleSameCardinality, 4},
		{RuleSameType, 5},
		{RuleSameOneof, 7},
		{RuleReservedReused, 10},
		{RuleReservedReused, 11},
	}
	if got := Check(prev, next); !slices.Equal(got, want) {
		t.Fatalf("Check(V1, V2Broken) =\n%v\nwant\n%v", got, want)
	}
}
