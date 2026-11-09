package sandbox

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Valkey песочницы: пользователь запуска видит только свой префикс, опасные
// команды ему закрыты, после запуска пользователь и ключи удаляются.
//
//	RUNNER_TEST_VALKEY=valkey://default:labs-valkey@127.0.0.1:56380
func TestLabsValkey(t *testing.T) {
	admin := os.Getenv("RUNNER_TEST_VALKEY")
	if admin == "" {
		t.Skip("нет RUNNER_TEST_VALKEY")
	}
	ac, _, err := valkeyAdmin(admin)
	if err != nil {
		t.Fatal(err)
	}
	defer ac.Close()
	ctx := context.Background()
	// Чужой ключ: программа запуска не должна его ни прочитать, ни стереть.
	if err := ac.Do(ctx, ac.B().Set().Key("foreign").Value("secret").Build()).Error(); err != nil {
		t.Fatal(err)
	}
	defer ac.Do(ctx, ac.B().Del().Key("foreign").Build())

	e := newExecutor(t)
	e.labs = newLabs(map[string]string{LabValkey: admin})
	src := `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/valkey-io/valkey-go"
)

func main() {
	ctx := context.Background()
	c, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{os.Getenv("VALKEY_ADDR")},
		Username:    os.Getenv("VALKEY_USERNAME"),
		Password:    os.Getenv("VALKEY_PASSWORD"),
		DisableCache: true,
	})
	if err != nil {
		panic(err)
	}
	p := os.Getenv("VALKEY_PREFIX")
	fmt.Println(c.Do(ctx, c.B().Set().Key(p+"k").Value("v").Build()).Error())
	v, _ := c.Do(ctx, c.B().Get().Key(p+"k").Build()).ToString()
	fmt.Println(v)
	fmt.Println(c.Do(ctx, c.B().Get().Key("foreign").Build()).Error() != nil)
	fmt.Println(c.Do(ctx, c.B().Flushall().Build()).Error() != nil)
	fmt.Println(c.Do(ctx, c.B().ConfigSet().ParameterValue().ParameterValue("maxmemory", "1").Build()).Error() != nil)
	fmt.Println(p)
}
`
	res, err := e.Run(ctx, Files{"main.go": src}, Flags{Labs: []string{LabValkey}})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	if res.ExitCode != 0 || len(lines) != 6 || strings.Join(lines[:5], ",") != "<nil>,v,true,true,true" {
		t.Fatalf("stdout %q, stderr %q", res.Stdout, res.Stderr)
	}
	prefix := lines[5]
	if !validLabID(strings.TrimSuffix(prefix, ":")) {
		t.Fatalf("префикс %q", prefix)
	}
	if v, _ := ac.Do(ctx, ac.B().Get().Key("foreign").Build()).ToString(); v != "secret" {
		t.Errorf("чужой ключ = %q", v)
	}
	if n, _ := ac.Do(ctx, ac.B().Exists().Key(prefix+"k").Build()).AsInt64(); n != 0 {
		t.Errorf("ключ запуска не удалён")
	}
	users, _ := ac.Do(ctx, ac.B().AclUsers().Build()).AsStrSlice()
	for _, u := range users {
		if u+":" == prefix {
			t.Errorf("пользователь %s не удалён", u)
		}
	}
}

// Запуск с labs: [postgres] при настроенной реплике получает и
// POSTGRES_REPLICA_DSN: та же база, только для чтения, с задержкой.
//
//	RUNNER_TEST_POSTGRES=postgres://runner:labs-runner@127.0.0.1:55432/postgres?sslmode=disable
//	RUNNER_TEST_POSTGRES_REPLICA=127.0.0.1:55433
func TestLabsPostgresReplica(t *testing.T) {
	admin, replica := os.Getenv("RUNNER_TEST_POSTGRES"), os.Getenv("RUNNER_TEST_POSTGRES_REPLICA")
	if admin == "" || replica == "" {
		t.Skip("нет RUNNER_TEST_POSTGRES или RUNNER_TEST_POSTGRES_REPLICA")
	}
	e := newExecutor(t)
	e.labs = newLabs(map[string]string{LabPostgres: admin, LabPostgresReplica: replica})
	src := `package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()
	p, err := pgx.Connect(ctx, os.Getenv("POSTGRES_DSN"))
	if err != nil {
		panic(err)
	}
	r, err := pgx.Connect(ctx, os.Getenv("POSTGRES_REPLICA_DSN"))
	if err != nil {
		panic(err)
	}
	p.Exec(ctx, "CREATE TABLE t (x int)")
	_, err = r.Exec(ctx, "CREATE TABLE u (x int)")
	fmt.Println(err != nil)
	for range 100 {
		var n int
		if r.QueryRow(ctx, "SELECT count(*) FROM t").Scan(&n) == nil {
			fmt.Println("доехала")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Println("не доехала")
}
`
	res, err := e.Run(context.Background(), Files{"main.go": src}, Flags{Labs: []string{LabPostgres}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "true\nдоехала\n" {
		t.Fatalf("stdout %q, stderr %q", res.Stdout, res.Stderr)
	}
	// База удалена и на реплике (после задержки применения).
	u, _ := url.Parse(admin)
	u.Host = replica
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := pgx.Connect(context.Background(), u.String())
		if err != nil {
			t.Fatal(err)
		}
		var n int
		conn.QueryRow(context.Background(), `SELECT count(*) FROM pg_database WHERE datname LIKE 'run\_%'`).Scan(&n)
		conn.Close(context.Background())
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("на реплике осталось баз запусков: %d", n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
