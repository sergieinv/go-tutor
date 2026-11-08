package task

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestConfigOverrides(t *testing.T) {
	msk := time.FixedZone("MSK", 3*60*60)
	cfg, err := Config(Settings{Host: "10.0.0.5", Port: 3307, User: "app", Database: "shop",
		Location: msk, ConnectTimeout: time.Second, IOTimeout: 2 * time.Second})
	if err != nil || cfg == nil {
		t.Fatalf("Config = %v, %v", cfg, err)
	}
	if cfg.Addr != "10.0.0.5:3307" || cfg.Loc != msk {
		t.Errorf("Addr=%q Loc=%v, want 10.0.0.5:3307 MSK", cfg.Addr, cfg.Loc)
	}
	if cfg.Timeout != time.Second || cfg.ReadTimeout != 2*time.Second || cfg.WriteTimeout != 2*time.Second {
		t.Errorf("таймауты: %v %v %v, want 1s 2s 2s", cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout)
	}
	// Остальное — значения по умолчанию из mysql.NewConfig.
	def := mysql.NewConfig()
	if cfg.MaxAllowedPacket != def.MaxAllowedPacket || !cfg.AllowNativePasswords || !cfg.CheckConnLiveness || cfg.InterpolateParams || cfg.MultiStatements {
		t.Errorf("изменены значения по умолчанию: %+v", cfg)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	// Пароль со служебными символами DSN и IPv6-адрес: склейка
	// строки через Sprintf сломала бы оба.
	s := Settings{Host: "::1", User: "app", Password: "p@ss:w/rd?x=1)", Database: "shop"}
	cfg, err := Config(s)
	if err != nil || cfg == nil {
		t.Fatalf("Config = %v, %v", cfg, err)
	}
	if cfg.Addr != "[::1]:3306" {
		t.Errorf("Addr = %q, want [::1]:3306", cfg.Addr)
	}
	back, err := mysql.ParseDSN(cfg.FormatDSN())
	if err != nil {
		t.Fatalf("ParseDSN(FormatDSN()) = %v", err)
	}
	if back.Passwd != s.Password || back.Addr != cfg.Addr || back.DBName != "shop" || !back.ParseTime || back.Collation != "utf8mb4_0900_ai_ci" {
		t.Errorf("DSN не пережил FormatDSN → ParseDSN: %+v", back)
	}
	if _, err := mysql.NewConnector(cfg); err != nil {
		t.Errorf("NewConnector: %v", err)
	}
}

func TestConfigInvalidRanges(t *testing.T) {
	base := Settings{Host: "db", User: "app", Database: "shop"}
	for name, mod := range map[string]func(*Settings){
		"порт -1":           func(s *Settings) { s.Port = -1 },
		"порт 65536":        func(s *Settings) { s.Port = 65536 },
		"ConnectTimeout -1": func(s *Settings) { s.ConnectTimeout = -time.Second },
		"IOTimeout -1":      func(s *Settings) { s.IOTimeout = -time.Second },
	} {
		s := base
		mod(&s)
		if cfg, err := Config(s); !errors.Is(err, ErrInvalidSettings) || cfg != nil {
			t.Errorf("%s: %v, %v; want nil, ErrInvalidSettings", name, cfg, err)
		}
	}
	s := base
	s.Port = 65535
	if _, err := Config(s); err != nil {
		t.Errorf("порт 65535: %v", err)
	}
}

func TestClassifyUnchanged(t *testing.T) {
	other := &mysql.MySQLError{Number: 1146, Message: "Table doesn't exist"}
	for _, err := range []error{other, errors.New("boom"), context.Canceled, mysql.ErrInvalidConn, sql.ErrNoRows} {
		got := Classify(err)
		if got != err {
			t.Errorf("Classify(%v) = %v, want ту же ошибку без изменений", err, got)
		}
		if errors.Is(got, ErrRetryable) || errors.Is(got, ErrDuplicate) {
			t.Errorf("Classify(%v) классифицирована: %v", err, got)
		}
	}
	if err := Classify(&mysql.MySQLError{Number: 1062}); errors.Is(err, ErrRetryable) {
		t.Errorf("дубликат помечен как повторяемый: %v", err)
	}
}

func TestRetryGivesUp(t *testing.T) {
	db := openDB(t)
	calls := 0
	var waits []int
	r := Retry{Attempts: 3, Wait: func(ctx context.Context, attempt int) error {
		waits = append(waits, attempt)
		return nil
	}}
	err := r.InTx(testCtx(t), db, func(ctx context.Context, tx *sql.Tx) error {
		calls++
		if _, err := tx.ExecContext(ctx, `INSERT INTO log VALUES ('x')`); err != nil {
			return err
		}
		return &mysql.MySQLError{Number: 1205, Message: "Lock wait timeout exceeded"}
	})
	var me *mysql.MySQLError
	if !errors.Is(err, ErrRetryable) || !errors.As(err, &me) || me.Number != 1205 {
		t.Errorf("InTx = %v, want ErrRetryable с исходной ошибкой 1205", err)
	}
	if calls != 3 || !slices.Equal(waits, []int{2, 3}) {
		t.Errorf("попыток %d, паузы %v; want 3 и [2 3] — без паузы после последней", calls, waits)
	}
	if n := count(t, db); n != 0 {
		t.Errorf("в таблице %d строк, want 0", n)
	}
}

func TestRetryNotRetryable(t *testing.T) {
	errBusiness := errors.New("out of stock")
	for _, tt := range []struct {
		ret, want error
	}{
		{&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}, ErrDuplicate},
		{errBusiness, errBusiness},
	} {
		db := openDB(t)
		calls := 0
		err := Retry{Attempts: 5}.InTx(testCtx(t), db, func(ctx context.Context, tx *sql.Tx) error {
			calls++
			tx.ExecContext(ctx, `INSERT INTO log VALUES ('x')`)
			return tt.ret
		})
		if !errors.Is(err, tt.want) || calls != 1 {
			t.Errorf("%v: InTx = %v после %d попыток, want errors.Is %v после 1", tt.ret, err, calls, tt.want)
		}
		if n := count(t, db); n != 0 {
			t.Errorf("%v: в таблице %d строк, want 0 — транзакция не откачена", tt.ret, n)
		}
	}
}

func TestRetryAttemptsBelowOne(t *testing.T) {
	db := openDB(t)
	calls := 0
	err := Retry{}.InTx(testCtx(t), db, func(ctx context.Context, tx *sql.Tx) error {
		calls++
		return &mysql.MySQLError{Number: 1213}
	})
	if calls != 1 || !errors.Is(err, ErrRetryable) {
		t.Errorf("Attempts: 0 — %d попыток, %v; want 1 и ErrRetryable", calls, err)
	}
}

func TestRetryWaitError(t *testing.T) {
	db := openDB(t)
	ctx, cancel := context.WithCancel(testCtx(t))
	calls := 0
	r := Retry{Attempts: 3, Wait: func(ctx context.Context, attempt int) error {
		cancel() // например, сервис останавливается во время паузы
		return ctx.Err()
	}}
	err := r.InTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		calls++
		return &mysql.MySQLError{Number: 1213}
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Errorf("InTx = %v после %d попыток, want context.Canceled после 1", err, calls)
	}
}

func TestRetryCanceledContext(t *testing.T) {
	db := openDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := Retry{Attempts: 3}.InTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Errorf("отменённый контекст: %v, fn вызвана: %v; want context.Canceled без вызова", err, called)
	}
}

func TestRetryCommitError(t *testing.T) {
	db := openDB(t)
	ctx := testCtx(t)
	for _, q := range []string{
		`CREATE TABLE parent (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE child (pid INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	// Отложенный внешний ключ проверяется при COMMIT: fn успешна,
	// а фиксация — нет. Ошибку Commit нельзя терять.
	err := Retry{Attempts: 2}.InTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO child VALUES (42)`)
		return err
	})
	if err == nil {
		t.Fatal("InTx = nil, want ошибку COMMIT")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM child`).Scan(&n); err != nil || n != 0 {
		t.Errorf("child: %d строк, %v; want 0", n, err)
	}
}

func TestRetryPanic(t *testing.T) {
	db := openDB(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("паника из fn не дошла до вызывающего")
			}
		}()
		Retry{Attempts: 3}.InTx(testCtx(t), db, func(ctx context.Context, tx *sql.Tx) error {
			tx.ExecContext(ctx, `INSERT INTO log VALUES ('x')`)
			panic("bug")
		})
	}()
	if n := count(t, db); n != 0 {
		t.Errorf("после паники в таблице %d строк, want 0", n)
	}
}

// TestMySQLReal — на настоящем MySQL профиля labs; без него пропускается.
func TestMySQLReal(t *testing.T) {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		t.Skip("MYSQL_DSN не задан — нужен MySQL из профиля labs")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(parsed.Addr)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := strconv.Atoi(port)
	msk := time.FixedZone("MSK", 3*60*60)
	cfg, err := Config(Settings{Host: host, Port: p, User: parsed.User, Password: parsed.Passwd,
		Database: parsed.DBName, Location: msk, IOTimeout: 5 * time.Second})
	if err != nil || cfg == nil {
		t.Fatalf("Config = %v, %v", cfg, err)
	}
	cfg.Params = map[string]string{"innodb_lock_wait_timeout": "3"}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	ctx := testCtx(t)
	for _, q := range []string{
		`DROP TABLE IF EXISTS items, acc`,
		`CREATE TABLE items (id INT PRIMARY KEY, name VARCHAR(20) NOT NULL, at DATETIME NOT NULL)`,
		`CREATE TABLE acc (id INT PRIMARY KEY, n INT NOT NULL)`,
		`INSERT INTO acc VALUES (1, 0), (2, 0)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	// utf8mb4 и время: эмодзи сохраняется, DATETIME читается
	// в time.Time в поясе Location.
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, msk)
	if _, err := db.ExecContext(ctx, `INSERT INTO items VALUES (1, ?, ?)`, "пицца 🍕", at); err != nil {
		t.Fatal(err)
	}
	var name string
	var got time.Time
	if err := db.QueryRowContext(ctx, `SELECT name, at FROM items WHERE id = 1`).Scan(&name, &got); err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if name != "пицца 🍕" || !got.Equal(at) || got.Location() != msk {
		t.Errorf("прочитано %q %v, want %q %v", name, got, "пицца 🍕", at)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO items VALUES (1, 'x', NOW())`)
	if err := Classify(err); !errors.Is(err, ErrDuplicate) {
		t.Errorf("повтор ключа: %v, want ErrDuplicate", err)
	}

	// Настоящий дедлок: каждая транзакция блокирует свою строку,
	// дожидается, пока другая заблокирует свою, и просит чужую.
	// InnoDB откатывает одну, Retry повторяет её — обе проходят.
	locked := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	var calls atomic.Int32
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, rows := range [2][2]int{{1, 2}, {2, 1}} {
		wg.Go(func() {
			attempt := 0
			errs[i] = Retry{Attempts: 3}.InTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
				calls.Add(1)
				attempt++
				if _, err := tx.ExecContext(ctx, `UPDATE acc SET n = n + 1 WHERE id = ?`, rows[0]); err != nil {
					return err
				}
				if attempt == 1 {
					close(locked[i])
					<-locked[1-i]
				}
				_, err := tx.ExecContext(ctx, `UPDATE acc SET n = n + 1 WHERE id = ?`, rows[1])
				return err
			})
		})
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || calls.Load() != 3 {
		t.Errorf("дедлок: ошибки %v, %v, попыток %d; want nil, nil, 3", errs[0], errs[1], calls.Load())
	}
	var sum int
	if err := db.QueryRowContext(ctx, `SELECT SUM(n) FROM acc`).Scan(&sum); err != nil || sum != 4 {
		t.Errorf("SUM(n) = %d, %v; want 4", sum, err)
	}
}
