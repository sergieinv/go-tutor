package features_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gotutor/labs/orders/internal/features"
)

func do(t *testing.T, h http.Handler, method, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "/admin/features", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func TestFeaturesEndpoint(t *testing.T) {
	f := features.New(true, false)
	h := f.Handler(slog.New(slog.DiscardHandler))

	if code, body := do(t, h, http.MethodGet, ""); code != 200 || body != `{"catalog_cache":true,"replica_reads":false}` {
		t.Errorf("GET = %d %s", code, body)
	}
	if code, body := do(t, h, http.MethodPost, `{"catalog_cache": false}`); code != 200 || body != `{"catalog_cache":false,"replica_reads":false}` {
		t.Errorf("POST = %d %s", code, body)
	}
	if f.CatalogCache() {
		t.Error("CatalogCache = true after switching off")
	}
	// Реплика не настроена — включить нельзя, остальное не меняется.
	if code, _ := do(t, h, http.MethodPost, `{"catalog_cache": true, "replica_reads": true}`); code != http.StatusConflict {
		t.Errorf("enable unconfigured replica: %d, want 409", code)
	}
	if f.CatalogCache() || f.ReplicaReads() {
		t.Error("flags changed by a rejected request")
	}
	for _, body := range []string{`{"cache": true}`, `not json`, `{"catalog_cache": "yes"}`} {
		if code, _ := do(t, h, http.MethodPost, body); code != http.StatusBadRequest {
			t.Errorf("POST %s = %d, want 400", body, code)
		}
	}
	if code, _ := do(t, h, http.MethodPost, `{"catalog_cache": true, "x": "`+strings.Repeat("a", 2000)+`"}`); code != http.StatusBadRequest {
		t.Errorf("oversized body = %d, want 400", code)
	}
	if code, _ := do(t, h, http.MethodDelete, ""); code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE = %d, want 405", code)
	}
}
