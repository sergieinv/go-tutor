package sandbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
)

func TestLabsNotConfigured(t *testing.T) {
	e := newExecutor(t)
	files := program(`fmt.Printf("[%s]\n", os.Getenv("POSTGRES_DSN"))`, "fmt", "os")
	res, err := e.Run(context.Background(), files, Flags{Labs: []string{LabPostgres}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "[]\n" || !strings.Contains(res.Stderr, "labs: postgres в этом runner не настроен") {
		t.Fatalf("stdout %q, stderr %q", res.Stdout, res.Stderr)
	}
}

func TestLabsUnavailable(t *testing.T) {
	e := newExecutor(t)
	e.labs = newLabs(map[string]string{LabPostgres: "postgres://postgres:x@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1"})
	res, err := e.Run(context.Background(), program(`fmt.Printf("[%s]\n", os.Getenv("POSTGRES_DSN"))`, "fmt", "os"), Flags{Labs: []string{LabPostgres}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "[]\n" || !strings.Contains(res.Stderr, "labs: postgres недоступен") {
		t.Fatalf("stdout %q, stderr %q", res.Stdout, res.Stderr)
	}
}

// Каждый запуск получает свою пустую базу, после запуска она удаляется.
// Нужен запущенный профиль labs:
//
//	RUNNER_TEST_POSTGRES=postgres://runner:labs-runner@127.0.0.1:55432/postgres?sslmode=disable
//	RUNNER_TEST_MYSQL='runner:labs-runner@tcp(127.0.0.1:53306)/'
func TestLabsPostgres(t *testing.T) {
	admin := os.Getenv("RUNNER_TEST_POSTGRES")
	if admin == "" {
		t.Skip("нет RUNNER_TEST_POSTGRES")
	}
	e := newExecutor(t)
	e.labs = newLabs(map[string]string{LabPostgres: admin})
	src := `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("POSTGRES_DSN"))
	if err != nil {
		panic(err)
	}
	var tables int
	conn.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE tablename = 'orders'").Scan(&tables)
	if _, err := conn.Exec(ctx, "CREATE TABLE orders (id int)"); err != nil {
		panic(err)
	}
	var db string
	conn.QueryRow(ctx, "SELECT current_database()").Scan(&db)
	// Соединение не закрыто намеренно: runner удалит базу и так.
	fmt.Println(tables, db)
}
`
	var dbs []string
	for range 2 {
		res, err := e.Run(context.Background(), Files{"main.go": src}, Flags{Labs: []string{LabPostgres}})
		if err != nil {
			t.Fatal(err)
		}
		tables, db, _ := strings.Cut(strings.TrimSpace(res.Stdout), " ")
		if res.ExitCode != 0 || tables != "0" || !validLabID(db) {
			t.Fatalf("%+v", res)
		}
		dbs = append(dbs, db)
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var left int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname = ANY($1)", dbs).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("базы %v не удалены", dbs)
	}
}

func TestLabsMySQL(t *testing.T) {
	admin := os.Getenv("RUNNER_TEST_MYSQL")
	if admin == "" {
		t.Skip("нет RUNNER_TEST_MYSQL")
	}
	e := newExecutor(t)
	e.labs = newLabs(map[string]string{LabMySQL: admin})
	src := `package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	db, err := sql.Open("mysql", os.Getenv("MYSQL_DSN"))
	if err != nil {
		panic(err)
	}
	if _, err := db.Exec("CREATE TABLE orders (id int)"); err != nil {
		panic(err)
	}
	// Транзакция с блокировкой остаётся открытой: удаление базы не должно её ждать.
	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}
	tx.Exec("INSERT INTO orders VALUES (1)")
	var name string
	db.QueryRow("SELECT DATABASE()").Scan(&name)
	fmt.Println(name)
}
`
	res, err := e.Run(context.Background(), Files{"main.go": src}, Flags{Labs: []string{LabMySQL}})
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimSpace(res.Stdout)
	if res.ExitCode != 0 || !validLabID(name) {
		t.Fatalf("%+v", res)
	}
	db, err := sql.Open("mysql", admin)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var left int
	if err := db.QueryRow("SELECT count(*) FROM information_schema.schemata WHERE schema_name = ?", name).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("база %s не удалена", name)
	}
}

func TestStaleIDs(t *testing.T) {
	now := uint32(time.Now().Unix())
	fresh := fmt.Sprintf("run_%08x%s", now, "0a0b0c0d")
	old := fmt.Sprintf("run_%08x%s", now-uint32(2*labStaleAge/time.Second), "0a0b0c0d")
	got := staleIDs([]string{fresh, old, "run_xyz", "runner", "run_0a0b"})
	if len(got) != 1 || got[0] != old {
		t.Fatalf("staleIDs = %v, want [%s]", got, old)
	}
}

// RUNNER_TEST_KAFKA=localhost:59092
func TestLabsKafka(t *testing.T) {
	brokers := os.Getenv("RUNNER_TEST_KAFKA")
	if brokers == "" {
		t.Skip("нет RUNNER_TEST_KAFKA")
	}
	e := newExecutor(t)
	e.cfg.WallTime = 10 * time.Second
	e.labs = newLabs(map[string]string{LabKafka: brokers})
	src := `package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	topic := os.Getenv("KAFKA_TOPIC_PREFIX") + "orders"
	cl, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(os.Getenv("KAFKA_BROKERS"), ",")...),
		kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		panic(err)
	}
	defer cl.Close()
	if _, err := kadm.NewClient(cl).CreateTopic(ctx, 1, 1, nil, topic); err != nil {
		panic(err)
	}
	if err := cl.ProduceSync(ctx, &kgo.Record{Topic: topic, Value: []byte("created")}).FirstErr(); err != nil {
		panic(err)
	}
	fs := cl.PollFetches(ctx)
	if err := fs.Err(); err != nil {
		panic(err)
	}
	fmt.Println(topic, string(fs.Records()[0].Value))
}
`
	res, err := e.Run(context.Background(), Files{"main.go": src}, Flags{Labs: []string{LabKafka}})
	if err != nil {
		t.Fatal(err)
	}
	topic, value, _ := strings.Cut(strings.TrimSpace(res.Stdout), " ")
	if res.ExitCode != 0 || value != "created" || !strings.HasPrefix(topic, labPrefix) {
		t.Fatalf("%+v", res)
	}
	adm, cl, err := kafkaAdmin(brokers)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	topics, err := adm.ListTopics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if topics.Has(topic) {
		t.Fatalf("топик %s не удалён", topic)
	}
}

// Роль runner (CREATE USER) не должна снимать блокировки входа из песочницы:
// иначе программа ученика удалила бы запись root@'172.29.99.%' и вошла как root.
// ACCOUNT LOCK на уже заблокированной записи ничего не меняет, даже если тест упадёт.
func TestLabsMySQLLockedAccounts(t *testing.T) {
	admin := os.Getenv("RUNNER_TEST_MYSQL")
	if admin == "" {
		t.Skip("нет RUNNER_TEST_MYSQL")
	}
	db, err := sql.Open("mysql", admin)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, acc := range []string{"'root'@'172.29.99.%'", "'warehouse'@'172.29.99.%'"} {
		_, err := db.Exec("ALTER USER " + acc + " ACCOUNT LOCK")
		var myErr *mysql.MySQLError
		if !errors.As(err, &myErr) || myErr.Number != 1227 {
			t.Errorf("ALTER USER %s от runner: %v, want ошибку 1227 (нужен SYSTEM_USER)", acc, err)
		}
	}
}
