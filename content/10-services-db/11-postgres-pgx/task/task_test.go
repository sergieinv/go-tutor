package task

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyCodes(t *testing.T) {
	cases := []struct {
		code string
		want error
	}{
		{"23505", ErrDuplicate},
		{"23503", ErrForeignKey},
		{"40001", ErrRetry},
		{"40P01", ErrRetry},
		{"57014", ErrCanceled},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			src := &pgconn.PgError{Code: c.code, ConstraintName: "items_pkey"}
			err := Classify(fmt.Errorf("insert item: %w", src))
			if !errors.Is(err, c.want) {
				t.Fatalf("Classify(%s) = %v, want errors.Is %v", c.code, err, c.want)
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.ConstraintName != "items_pkey" {
				t.Fatalf("Classify(%s): исходная *pgconn.PgError потеряна: %v", c.code, err)
			}
		})
	}
}

func TestClassifyPassThrough(t *testing.T) {
	if err := Classify(nil); err != nil {
		t.Fatalf("Classify(nil) = %v, want nil", err)
	}
	plain := errors.New("connection refused")
	if err := Classify(plain); err != plain {
		t.Fatalf("Classify(plain) = %v, want тот же err", err)
	}
}

func TestBuildUpsert(t *testing.T) {
	got, err := BuildUpsert("items", []string{"sku", "name", "price"}, []string{"sku"}, []string{"name", "price"})
	want := `INSERT INTO "items" ("sku", "name", "price") VALUES ($1, $2, $3) ON CONFLICT ("sku") DO UPDATE SET "name" = EXCLUDED."name", "price" = EXCLUDED."price"`
	if err != nil || got != want {
		t.Fatalf("BuildUpsert = (%q, %v)\nwant %q", got, err, want)
	}

	got, err = BuildUpsert("tags", []string{"item_id", "tag"}, []string{"item_id", "tag"}, nil)
	want = `INSERT INTO "tags" ("item_id", "tag") VALUES ($1, $2) ON CONFLICT ("item_id", "tag") DO NOTHING`
	if err != nil || got != want {
		t.Fatalf("BuildUpsert = (%q, %v)\nwant %q", got, err, want)
	}
}

func TestBuildUpsertRejectsInjection(t *testing.T) {
	got, err := BuildUpsert("items", []string{"sku", `name"; DROP TABLE items; --`}, []string{"sku"}, nil)
	if !errors.Is(err, ErrInvalidUpsert) || got != "" {
		t.Fatalf("BuildUpsert = (%q, %v), want (\"\", ErrInvalidUpsert)", got, err)
	}
}
