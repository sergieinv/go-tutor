package task

import "testing"

func TestIsExported(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"User", true},
		{"user", false},
		{"MaxItems", true},
	}
	for _, tt := range tests {
		if got := IsExported(tt.name); got != tt.want {
			t.Errorf("IsExported(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestCanImport(t *testing.T) {
	tests := []struct {
		importer, imported string
		want               bool
	}{
		{"example.com/shop/cmd/api", "example.com/shop/order", true},
		{"example.com/shop/cmd/api", "example.com/shop/internal/money", true},
		{"example.com/billing", "example.com/shop/internal/money", false},
	}
	for _, tt := range tests {
		if got := CanImport(tt.importer, tt.imported); got != tt.want {
			t.Errorf("CanImport(%q, %q) = %v, want %v", tt.importer, tt.imported, got, tt.want)
		}
	}
}
