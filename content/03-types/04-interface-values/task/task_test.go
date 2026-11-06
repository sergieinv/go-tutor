package task

import "testing"

func TestFirstError(t *testing.T) {
	a := &FieldError{"name", "empty"}
	b := &FieldError{"age", "negative"}
	if got := FirstError(nil, a, b); got != a {
		t.Errorf("FirstError(nil, a, b) = %v, want %v", got, a)
	}
	if got := FirstError(nil, nil); got != nil {
		t.Errorf("FirstError(nil, nil) = %#v, want nil", got)
	}
}

func TestKind(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, "nil"},
		{"int", 7, "int"},
		{"string", "go", "string"},
		{"error", &FieldError{"a", "b"}, "error"},
		{"float", 1.5, "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Kind(tt.v); got != tt.want {
				t.Errorf("Kind(%#v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}
