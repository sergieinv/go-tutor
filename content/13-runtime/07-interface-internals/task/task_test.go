package task

import "testing"

func TestKindsAndAccessors(t *testing.T) {
	if v := Int64Value(-42); v.Kind() != KindInt64 || v.Int64() != -42 {
		t.Errorf("Int64Value(-42): Kind = %v, Int64 = %d", v.Kind(), v.Int64())
	}
	if v := BoolValue(true); v.Kind() != KindBool || !v.Bool() {
		t.Errorf("BoolValue(true): Kind = %v, Bool = %v", v.Kind(), v.Bool())
	}
	if v := StringValue("go"); v.Kind() != KindString || v.Str() != "go" {
		t.Errorf("StringValue(go): Kind = %v, Str = %q", v.Kind(), v.Str())
	}
	if v := AnyValue(3.5); v.Kind() != KindAny || v.Any() != 3.5 {
		t.Errorf("AnyValue(3.5): Kind = %v, Any = %v", v.Kind(), v.Any())
	}
}

func TestEqualBasics(t *testing.T) {
	if !Int64Value(7).Equal(Int64Value(7)) {
		t.Errorf("Int64Value(7) != Int64Value(7)")
	}
	if Int64Value(7).Equal(StringValue("7")) {
		t.Errorf("значения разных видов не должны быть равны")
	}
	if !StringValue("a").Equal(StringValue("a")) || StringValue("a").Equal(StringValue("b")) {
		t.Errorf("сравнение строк работает неверно")
	}
}

var sink Value

func TestInt64NoAllocs(t *testing.T) {
	nums := []int64{1 << 40, -1000, 123456789}
	i := 0
	allocs := testing.AllocsPerRun(100, func() {
		sink = Int64Value(nums[i%len(nums)])
		i++
	})
	if allocs != 0 {
		t.Errorf("Int64Value в глобальную переменную: %v аллокаций, want 0", allocs)
	}
}
