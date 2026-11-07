// Внутренний тестовый пакет — тот же `package shop`:
// тест видит неэкспортированные функции и поля. Оба вида
// тестов могут лежать в одном каталоге.
package shop

import "testing"

func TestNormalizeSKU(t *testing.T) {
	got := normalizeSKU("  ab-12 ")
	if got != "AB-12" {
		t.Errorf("normalizeSKU(%q) = %q, want %q", "  ab-12 ", got, "AB-12")
	}
}
