package task

import (
	"testing"
	"time"
)

func TestFirstErrorEmpty(t *testing.T) {
	if got := FirstError(); got != nil {
		t.Errorf("FirstError() = %#v, want nil-интерфейс", got)
	}
	var none []*FieldError
	if got := FirstError(none...); got != nil {
		t.Errorf("FirstError(nil-срез...) = %#v, want nil-интерфейс", got)
	}
}

// both реализует и error, и fmt.Stringer.
type both struct{}

func (both) Error() string  { return "both" }
func (both) String() string { return "both" }

type intLike int

func TestKindTraps(t *testing.T) {
	var nilErr error
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"nil error interface", nilErr, "nil"},
		{"typed nil pointer", (*FieldError)(nil), "error"},
		{"error and stringer", both{}, "error"},
		{"stringer", time.Second, "stringer"},
		{"named int", intLike(1), "other"},
		{"int64", int64(1), "other"},
		{"slice", []int{1}, "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Kind(tt.v); got != tt.want {
				t.Errorf("Kind(%#v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}
