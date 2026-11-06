package task

import (
	"errors"
	"flag"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseHelp(t *testing.T) {
	for _, args := range []string{"add -h", "list -help", "add --help"} {
		got, err := Parse(strings.Fields(args))
		if !errors.Is(err, flag.ErrHelp) {
			t.Errorf("Parse(%q) вернул %v, want flag.ErrHelp", args, err)
		}
		if !reflect.DeepEqual(got, Command{}) {
			t.Errorf("Parse(%q) при ошибке вернул %+v, want Command{}", args, got)
		}
	}
}

func TestParseFlagsAreSeparate(t *testing.T) {
	for _, args := range []string{"add -all текст", "list -p 2", "list -tag x", "add -limit 3 текст", "add -unknown текст"} {
		got, err := Parse(strings.Fields(args))
		if err == nil {
			t.Errorf("Parse(%q) = %+v, want ошибку: флаг чужой подкоманды", args, got)
		}
		if !reflect.DeepEqual(got, Command{}) {
			t.Errorf("Parse(%q) при ошибке вернул %+v, want Command{}", args, got)
		}
	}
}

func TestParseBadValues(t *testing.T) {
	for _, args := range []string{"add -p abc текст", "add -p 0 текст", "list -limit -1", "list -limit x", "list -all=maybe"} {
		got, err := Parse(strings.Fields(args))
		if err == nil || !reflect.DeepEqual(got, Command{}) {
			t.Errorf("Parse(%q) = %+v, %v; want Command{} и ошибку", args, got, err)
		}
	}
}

func TestParsePositionalStopsFlags(t *testing.T) {
	got, err := Parse([]string{"add", "позвонить", "-p", "2"})
	want := Command{Name: "add", Priority: 1, Text: "позвонить -p 2"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %+v, %v; want %+v — флаги после текста считаются текстом", got, err, want)
	}
	got, err = Parse([]string{"add", "-p", "4", "--", "-флаг-в-тексте"})
	want = Command{Name: "add", Priority: 4, Text: "-флаг-в-тексте"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(с --) = %+v, %v; want %+v", got, err, want)
	}
}

func TestParseTagsNilWhenAbsent(t *testing.T) {
	got, err := Parse([]string{"add", "x"})
	if err != nil || got.Tags != nil {
		t.Errorf("Parse(add x).Tags = %#v, %v; want nil", got.Tags, err)
	}
}

func TestParseBoolFlagForm(t *testing.T) {
	got, err := Parse([]string{"list", "-all=false", "-limit=2"})
	want := Command{Name: "list", Limit: 2}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %+v, %v; want %+v", got, err, want)
	}
}

// captureStderr перенаправляет os.Stderr на время f и возвращает, что туда попало.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stderr = old }()
	f()
	w.Close()
	return <-done
}

func TestParseSilent(t *testing.T) {
	out := captureStderr(t, func() {
		Parse([]string{"add", "-unknown", "x"})
		Parse([]string{"list", "-h"})
		Parse([]string{"list", "-limit", "x"})
	})
	if out != "" {
		t.Errorf("Parse печатает в stderr:\n%s\nвызовите SetOutput(io.Discard) у FlagSet", out)
	}
}
