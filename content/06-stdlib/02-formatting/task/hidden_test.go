package task

import (
	"fmt"
	"testing"
)

func TestMoneyNegative(t *testing.T) {
	tests := []struct {
		in   Money
		want string
	}{
		{-5, "-0.05"},
		{-1250, "-12.50"},
		{-100, "-1.00"},
		{-99, "-0.99"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Money(%d).String() = %q, want %q", int64(tt.in), got, tt.want)
		}
	}
}

func TestMoneyIsStringer(t *testing.T) {
	// String объявлен на значении — fmt вызывает его для Money и *Money.
	m := Money(305)
	if got := fmt.Sprintf("%v %s %v", m, m, &m); got != "3.05 3.05 3.05" {
		t.Errorf("fmt с Money = %q, want %q", got, "3.05 3.05 3.05")
	}
}

func TestReceiptCyrillicAndLong(t *testing.T) {
	got := Receipt([]Line{{"кофе", 2, 15000}, {"очень-длинное-имя", 1000, -1}})
	want := "" +
		"кофе           2      300.00\n" +
		"очень-длинное-имя1000      -10.00\n" +
		"TOTAL                 290.00\n"
	if got != want {
		t.Errorf("Receipt =\n%q\nwant\n%q", got, want)
	}
}

func TestReceiptEmpty(t *testing.T) {
	want := "TOTAL                   0.00\n"
	if got := Receipt(nil); got != want {
		t.Errorf("Receipt(nil) = %q, want %q", got, want)
	}
	if got := Receipt([]Line{}); got != want {
		t.Errorf("Receipt([]) = %q, want %q", got, want)
	}
}

func TestReceiptNegativeTotal(t *testing.T) {
	got := Receipt([]Line{{"возврат", 1, -45}})
	want := "" +
		"возврат        1       -0.45\n" +
		"TOTAL                  -0.45\n"
	if got != want {
		t.Errorf("Receipt =\n%q\nwant\n%q", got, want)
	}
}
