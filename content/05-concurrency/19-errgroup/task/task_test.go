package task

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func length(_ context.Context, s string) (int, error) { return len(s), nil }

func TestProcessAllOrder(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		limit int
		want  []int
	}{
		{"без лимита", []string{"a", "bbb", "cc"}, 0, []int{1, 3, 2}},
		{"лимит 1", []string{"go", "rust", "c"}, 1, []int{2, 4, 1}},
		{"лимит 2", []string{"aaaa", "", "bb", "c"}, 2, []int{4, 0, 2, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProcessAll(context.Background(), tt.items, tt.limit, length)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Errorf("ProcessAll(%q, %d) = (%v, %v), want (%v, nil)", tt.items, tt.limit, got, err, tt.want)
			}
		})
	}
}

func TestProcessAllError(t *testing.T) {
	bad := errors.New("bad item")
	f := func(_ context.Context, s string) (int, error) {
		if s == "bad" {
			return 0, bad
		}
		return len(s), nil
	}
	got, err := ProcessAll(context.Background(), []string{"ok", "bad", "fine"}, 0, f)
	if !errors.Is(err, bad) || got != nil {
		t.Fatalf("ProcessAll = (%v, %v), want (nil, bad item)", got, err)
	}
}
