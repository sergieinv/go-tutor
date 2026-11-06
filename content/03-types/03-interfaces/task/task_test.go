package task

import "testing"

func TestMapGetter(t *testing.T) {
	m := MapGetter{"host": "localhost"}
	if v, ok := m.Get("host"); !ok || v != "localhost" {
		t.Errorf(`Get("host") = %q, %v; want "localhost", true`, v, ok)
	}
	if v, ok := m.Get("port"); ok || v != "" {
		t.Errorf(`Get("port") = %q, %v; want "", false`, v, ok)
	}
}

func TestChainOrder(t *testing.T) {
	env := MapGetter{"port": "9090"}
	defaults := MapGetter{"port": "8080", "host": "localhost"}
	var g Getter = Chain{env, defaults}
	tests := []struct{ key, want string }{
		{"port", "9090"},
		{"host", "localhost"},
	}
	for _, tt := range tests {
		if got := GetOr(g, tt.key, "?"); got != tt.want {
			t.Errorf("GetOr(chain, %q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
