package task

import "testing"

func TestNilMapGetter(t *testing.T) {
	var m MapGetter
	if v, ok := m.Get("x"); ok || v != "" {
		t.Errorf(`nil MapGetter: Get("x") = %q, %v; want "", false`, v, ok)
	}
}

func TestChainEdges(t *testing.T) {
	if _, ok := (Chain{}).Get("x"); ok {
		t.Error("пустая Chain нашла значение")
	}
	var nilChain Chain
	if _, ok := nilChain.Get("x"); ok {
		t.Error("nil Chain нашла значение")
	}
	c := Chain{nil, MapGetter{"a": "1"}, nil}
	if v, ok := c.Get("a"); !ok || v != "1" {
		t.Errorf(`Chain с nil-элементами: Get("a") = %q, %v; want "1", true`, v, ok)
	}
}

func TestChainEmptyValueCounts(t *testing.T) {
	// Пустая строка — тоже найденное значение, ищем по ok, а не по v != "".
	c := Chain{MapGetter{"a": ""}, MapGetter{"a": "fallback"}}
	if v, ok := c.Get("a"); !ok || v != "" {
		t.Errorf(`Get("a") = %q, %v; want "", true`, v, ok)
	}
	if got := GetOr(c, "a", "def"); got != "" {
		t.Errorf(`GetOr(c, "a", "def") = %q, want ""`, got)
	}
}

func TestNestedChain(t *testing.T) {
	inner := Chain{MapGetter{"x": "inner"}}
	outer := Chain{MapGetter{"y": "outer"}, inner}
	if got := GetOr(outer, "x", "def"); got != "inner" {
		t.Errorf(`вложенная Chain: GetOr("x") = %q, want "inner"`, got)
	}
	if got := GetOr(outer, "z", "def"); got != "def" {
		t.Errorf(`GetOr("z") = %q, want "def"`, got)
	}
}
