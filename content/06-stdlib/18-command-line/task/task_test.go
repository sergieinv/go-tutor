package task

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		args string
		want Command
	}{
		{"add купить молоко", Command{Name: "add", Priority: 1, Text: "купить молоко"}},
		{"add -p 3 -tag home -tag shop купить хлеб", Command{Name: "add", Priority: 3, Tags: []string{"home", "shop"}, Text: "купить хлеб"}},
		{"add --p=5 срочно", Command{Name: "add", Priority: 5, Text: "срочно"}},
		{"list", Command{Name: "list", Limit: 10}},
		{"list -all -limit 3", Command{Name: "list", All: true, Limit: 3}},
	}
	for _, tt := range tests {
		got, err := Parse(strings.Fields(tt.args))
		if err != nil {
			t.Errorf("Parse(%q) вернул ошибку %v", tt.args, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Parse(%q) = %+v, want %+v", tt.args, got, tt.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse(nil); !errors.Is(err, ErrNoCommand) {
		t.Errorf("Parse(nil) вернул %v, want ErrNoCommand", err)
	}
	if _, err := Parse([]string{"remove", "1"}); !errors.Is(err, ErrUnknownCommand) {
		t.Errorf("Parse(remove) вернул %v, want ErrUnknownCommand", err)
	}
	for _, args := range []string{"add", "add -p 9 текст", "list -limit 0", "list extra"} {
		if got, err := Parse(strings.Fields(args)); err == nil {
			t.Errorf("Parse(%q) = %+v, want ошибку", args, got)
		}
	}
}
