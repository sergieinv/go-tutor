package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

func TestConfig(t *testing.T) {
	cfg, err := Config(Settings{Host: "db", User: "app", Password: "secret", Database: "shop"})
	if err != nil || cfg == nil {
		t.Fatalf("Config = %v, %v", cfg, err)
	}
	if cfg.Net != "tcp" || cfg.Addr != "db:3306" || cfg.User != "app" || cfg.Passwd != "secret" || cfg.DBName != "shop" {
		t.Errorf("адрес и учётные данные: net=%q addr=%q user=%q passwd=%q db=%q", cfg.Net, cfg.Addr, cfg.User, cfg.Passwd, cfg.DBName)
	}
	if !cfg.ParseTime || cfg.Loc != time.UTC || cfg.Collation != "utf8mb4_0900_ai_ci" {
		t.Errorf("ParseTime=%v Loc=%v Collation=%q, want true UTC utf8mb4_0900_ai_ci", cfg.ParseTime, cfg.Loc, cfg.Collation)
	}
	if cfg.Timeout != 5*time.Second || cfg.ReadTimeout != 30*time.Second || cfg.WriteTimeout != 30*time.Second {
		t.Errorf("таймауты: %v %v %v, want 5s 30s 30s", cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout)
	}
}

func TestConfigInvalid(t *testing.T) {
	for _, s := range []Settings{
		{User: "app", Database: "shop"},
		{Host: "db", Database: "shop"},
		{Host: "db", User: "app"},
	} {
		if cfg, err := Config(s); !errors.Is(err, ErrInvalidSettings) || cfg != nil {
			t.Errorf("Config(%+v) = %v, %v; want nil, ErrInvalidSettings", s, cfg, err)
		}
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		number uint16
		want   error
	}{
		{1062, ErrDuplicate},
		{1213, ErrRetryable},
		{1205, ErrRetryable},
	}
	for _, tt := range tests {
		err := Classify(fmt.Errorf("save order: %w", &mysql.MySQLError{Number: tt.number}))
		if !errors.Is(err, tt.want) {
			t.Errorf("%d: %v, want errors.Is %v", tt.number, err, tt.want)
		}
		var me *mysql.MySQLError
		if !errors.As(err, &me) || me.Number != tt.number {
			t.Errorf("%d: исходная *mysql.MySQLError потеряна: %v", tt.number, err)
		}
	}
	if Classify(nil) != nil {
		t.Error("Classify(nil) != nil")
	}
}

// openDB — SQLite во временном каталоге с одним соединением в пуле:
// незавершённая транзакция заблокирует следующую.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE log (msg TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func count(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(testCtx(t), `SELECT COUNT(*) FROM log`).Scan(&n); err != nil {
		t.Fatalf("SELECT COUNT(*): %v (соединение не вернулось в пул?)", err)
	}
	return n
}

func TestRetryDeadlockThenSuccess(t *testing.T) {
	db := openDB(t)
	calls := 0
	err := Retry{Attempts: 3}.InTx(testCtx(t), db, func(ctx context.Context, tx *sql.Tx) error {
		calls++
		if _, err := tx.ExecContext(ctx, `INSERT INTO log VALUES (?)`, fmt.Sprint("попытка ", calls)); err != nil {
			return err
		}
		if calls < 3 {
			return &mysql.MySQLError{Number: 1213, Message: "Deadlock found"}
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("InTx = %v после %d попыток, want nil после 3", err, calls)
	}
	if n := count(t, db); n != 1 {
		t.Errorf("в таблице %d строк, want 1: неудачные попытки должны откатываться", n)
	}
}
