package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestRunCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"product":{}}`)
	}))
	defer srv.Close()
	for _, model := range []string{"open", "closed"} {
		var out strings.Builder
		err := run(t.Context(), []string{"run", "-model", model, "-mix", "catalog", "-rate", "200", "-users", "2",
			"-think", "1ms", "-duration", "100ms", "-seed", "1"}, env(map[string]string{"LOADGEN_TARGET": srv.URL}), &out)
		if err != nil {
			t.Fatalf("%s: %v\n%s", model, err, out.String())
		}
		if !strings.Contains(out.String(), "catalog.get") || !strings.Contains(out.String(), "200:") {
			t.Errorf("%s report:\n%s", model, out.String())
		}
	}
}

func TestRunRejectsBadArgs(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"fly"},
		{"run", "-model", "both"},
		{"run", "-mix", "nope"},
		{"run", "-rate", "0"},
		{"run", "-order-products", "a,b"},
		{"analyze", "-rate", "-1"},
	} {
		var out strings.Builder
		if err := run(t.Context(), args, env(nil), &out); err == nil {
			t.Errorf("run(%q): want error", args)
		}
	}
	var out strings.Builder
	if err := run(t.Context(), []string{"help"}, env(nil), &out); err != nil || !strings.Contains(out.String(), "analyze") {
		t.Errorf("help: %v %s", err, out.String())
	}
}
