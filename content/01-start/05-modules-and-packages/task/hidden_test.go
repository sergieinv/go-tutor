package task

import "testing"

func TestIsExportedEdges(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"", false},
		{"_User", false},
		{"Ёлка", true},
		{"ёлка", false},
		{"Σ", true},
		{"X", true},
		{"x", false},
		{"userID", false},
		{"9Lives", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsExported(tt.name); got != tt.want {
				t.Errorf("IsExported(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestCanImportEdges(t *testing.T) {
	tests := []struct {
		name               string
		importer, imported string
		want               bool
	}{
		{"parent itself", "example.com/shop", "example.com/shop/internal/money", true},
		{"inside internal", "example.com/shop/internal/money/fmt", "example.com/shop/internal/money", true},
		{"sibling internal", "example.com/shop/internal/db", "example.com/shop/internal/money", true},
		{"string prefix is not tree", "example.com/shop2/billing", "example.com/shop/internal/money", false},
		{"string prefix exact", "example.com/shop2", "example.com/shop/internal/money", false},
		{"nested allowed", "example.com/shop/api/v1", "example.com/shop/api/internal/auth", true},
		{"nested denied", "example.com/shop/web", "example.com/shop/api/internal/auth", false},
		{"internal at end allowed", "example.com/shop/cmd", "example.com/shop/internal", true},
		{"internal at end denied", "example.com/other", "example.com/shop/internal", false},
		{"last internal wins denied", "example.com/shop/internal/z", "example.com/shop/internal/x/internal/y", false},
		{"last internal wins allowed", "example.com/shop/internal/x/sub", "example.com/shop/internal/x/internal/y", true},
		{"internals is not internal", "example.com/other", "example.com/shop/internals/x", true},
		{"myinternal is not internal", "example.com/other", "example.com/shop/myinternal", true},
		{"internal suffix in element", "example.com/other", "example.com/shop/xinternal/y", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanImport(tt.importer, tt.imported); got != tt.want {
				t.Errorf("CanImport(%q, %q) = %v, want %v", tt.importer, tt.imported, got, tt.want)
			}
		})
	}
}
