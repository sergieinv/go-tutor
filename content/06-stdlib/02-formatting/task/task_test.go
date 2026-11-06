package task

import "testing"

func TestMoneyString(t *testing.T) {
	tests := []struct {
		in   Money
		want string
	}{
		{1250, "12.50"},
		{7, "0.07"},
		{0, "0.00"},
		{100, "1.00"},
		{123456789, "1234567.89"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("Money(%d).String() = %q, want %q", int64(tt.in), got, tt.want)
		}
	}
}

func TestReceipt(t *testing.T) {
	got := Receipt([]Line{{"coffee", 2, 15000}, {"bun", 3, 4550}})
	want := "" +
		"coffee         2      300.00\n" +
		"bun            3      136.50\n" +
		"TOTAL                 436.50\n"
	if got != want {
		t.Errorf("Receipt =\n%s\nwant\n%s", got, want)
	}
}
