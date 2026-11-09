package task

import (
	"fmt"
	"testing"
)

var (
	sinkValues [4]Value
	sinkBool   bool
	sinkInt    int64
	sinkStr    string
	sinkKind   Kind
)

func TestScalarsNoAllocs(t *testing.T) {
	nums := []int64{1 << 40, -1000, 123456789, 1 << 62}
	strs := []string{"hello, gopher", "runtime", "interface internals", "x"}
	flags := []bool{true, false}
	i := 0
	cases := map[string]func(){
		"Int64Value":  func() { sinkValues[i%4] = Int64Value(nums[i%4]) },
		"BoolValue":   func() { sinkValues[i%4] = BoolValue(flags[i%2]) },
		"StringValue": func() { sinkValues[i%4] = StringValue(strs[i%4]) },
		"accessors": func() {
			sinkInt = Int64Value(nums[i%4]).Int64()
			sinkStr = StringValue(strs[i%4]).Str()
			sinkBool = BoolValue(flags[i%2]).Bool()
			sinkKind = StringValue(strs[i%4]).Kind()
		},
		"Equal": func() {
			sinkBool = Int64Value(nums[i%4]).Equal(Int64Value(nums[(i+1)%4])) ||
				StringValue(strs[i%4]).Equal(StringValue(strs[i%4])) ||
				BoolValue(flags[i%2]).Equal(Int64Value(nums[i%4]))
		},
	}
	for name, f := range cases {
		allocs := testing.AllocsPerRun(100, func() { f(); i++ })
		if allocs != 0 {
			t.Errorf("%s: %v аллокаций на вызов, want 0", name, allocs)
		}
	}
}

func TestValuesSurviveEscape(t *testing.T) {
	// Значения хранятся в глобальном массиве и читаются после — без порчи.
	sinkValues[0] = Int64Value(1 << 40)
	sinkValues[1] = StringValue("kept")
	sinkValues[2] = BoolValue(false)
	if sinkValues[0].Int64() != 1<<40 || sinkValues[1].Str() != "kept" || sinkValues[2].Bool() {
		t.Errorf("значения испортились: %v %v %v", sinkValues[0].Any(), sinkValues[1].Any(), sinkValues[2].Any())
	}
}

func TestZeroValue(t *testing.T) {
	var v Value
	if v.Kind() != KindAny || v.Any() != nil {
		t.Errorf("Value{}: Kind = %v, Any = %v; want KindAny, nil", v.Kind(), v.Any())
	}
	if !v.Equal(AnyValue(nil)) {
		t.Errorf("Value{} должно быть равно AnyValue(nil)")
	}
	if KindAny != 0 || KindInt64 != 1 || KindBool != 2 || KindString != 3 {
		t.Errorf("порядок констант Kind: %d %d %d %d", KindAny, KindInt64, KindBool, KindString)
	}
}

func TestAnyKeepsKindAny(t *testing.T) {
	v := AnyValue(int64(5))
	if v.Kind() != KindAny {
		t.Errorf("AnyValue(int64(5)).Kind() = %v, want KindAny", v.Kind())
	}
	if v.Equal(Int64Value(5)) {
		t.Errorf("AnyValue(int64(5)) и Int64Value(5) — разные виды, Equal должен вернуть false")
	}
}

func TestAnyReturnsTypedValues(t *testing.T) {
	tests := []struct {
		v    Value
		want any
	}{
		{Int64Value(5), int64(5)},
		{Int64Value(-1 << 50), int64(-1 << 50)},
		{BoolValue(true), true},
		{BoolValue(false), false},
		{StringValue(""), ""},
		{StringValue("go"), "go"},
		{AnyValue(uint8(3)), uint8(3)},
	}
	for _, tt := range tests {
		if got := tt.v.Any(); got != tt.want {
			t.Errorf("Any() = %#v (%T), want %#v (%T)", got, got, tt.want, tt.want)
		}
	}
}

func TestWrongKindPanics(t *testing.T) {
	calls := map[string]func(){
		"StringValue.Int64": func() { StringValue("1").Int64() },
		"Int64Value.Bool":   func() { Int64Value(1).Bool() },
		"BoolValue.Str":     func() { BoolValue(true).Str() },
		"AnyValue.Int64":    func() { AnyValue(int64(1)).Int64() },
	}
	for name, f := range calls {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s должен паниковать", name)
				}
			}()
			f()
		}()
	}
}

func TestEqualAny(t *testing.T) {
	type point struct{ X, Y int }
	type withSlice struct{ S []int }
	tests := []struct {
		name string
		a, b Value
		want bool
	}{
		{"equal ints", AnyValue(1), AnyValue(1), true},
		{"int vs string", AnyValue(1), AnyValue("1"), false},
		{"int vs int64", AnyValue(1), AnyValue(int64(1)), false},
		{"structs", AnyValue(point{1, 2}), AnyValue(point{1, 2}), true},
		{"different structs", AnyValue(point{1, 2}), AnyValue(point{2, 1}), false},
		{"nil vs nil", AnyValue(nil), AnyValue(nil), true},
		{"nil vs value", AnyValue(nil), AnyValue(0), false},
		{"slices", AnyValue([]int{1}), AnyValue([]int{1}), false},
		{"maps", AnyValue(map[string]int{}), AnyValue(map[string]int{}), false},
		{"funcs", AnyValue(func() {}), AnyValue(func() {}), false},
		{"struct with slice", AnyValue(withSlice{}), AnyValue(withSlice{}), false},
		{"slice vs int", AnyValue([]int{1}), AnyValue(1), false},
		{"bools", BoolValue(false), BoolValue(false), true},
		{"bool vs bool", BoolValue(true), BoolValue(false), false},
		{"pointer", AnyValue(&point{}), AnyValue(&point{}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Fatalf("Equal паникует: %v", p)
					}
				}()
				got = tt.a.Equal(tt.b)
			}()
			if got != tt.want {
				t.Errorf("%v.Equal(%v) = %v, want %v", fmt.Sprint(tt.a.Any()), fmt.Sprint(tt.b.Any()), got, tt.want)
			}
		})
	}
}
