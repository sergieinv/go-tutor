package sandbox

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
)

// Сервисы профиля labs (docker compose --profile labs up). Урок просит
// их флагом runner.labs, и на каждый запуск runner создаёт отдельную пустую
// базу со своим пользователем (у Kafka — свой префикс топиков и групп), а
// после запуска удаляет: запуски не видят данных друг друга, и упавшая
// программа не оставляет мусора.
const (
	LabPostgres = "postgres"
	LabMySQL    = "mysql"
	LabKafka    = "kafka"
)

// LabNames — допустимые значения runner.labs.
var LabNames = []string{LabPostgres, LabMySQL, LabKafka}

// labEnv — переменная окружения, в которой программа получает DSN своей базы.
var labEnv = map[string]string{
	LabPostgres: "POSTGRES_DSN",
	LabMySQL:    "MYSQL_DSN",
}

// labPrefix — имена временных баз и пользователей: run_<время создания,
// 8 hex><случайные 8 hex>. По префиксу и времени runner подчищает то, что
// осталось после аварийной остановки, не трогая идущие запуски других
// процессов (второй runner, параллельный lessonlint).
const labPrefix = "run_"

// labStaleAge — база старше этого точно не принадлежит идущему запуску:
// запуск ограничен секундами.
const labStaleAge = 10 * time.Minute

// labConnLimit — сколько соединений может открыть программа к своей базе.
const labConnLimit = 20

// labTimeout — сколько ждать создания или удаления базы. Профиль labs
// не запущен — DNS-имя сервиса не резолвится, и ошибка приходит сразу.
const labTimeout = 5 * time.Second

type labs struct {
	admin map[string]string // имя → DSN администратора

	mu      sync.Mutex
	cleaned map[string]bool // старые базы уже убраны
}

func newLabs(admin map[string]string) *labs {
	return &labs{admin: admin, cleaned: map[string]bool{}}
}

// cleanOnce убирает старые базы при первом успешном подключении: если
// профиль labs поднялся позже runner, уборка повторится при следующем запуске.
func (l *labs) cleanOnce(name, admin string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.cleaned[name] && l.dropStale(name, admin) == nil {
		l.cleaned[name] = true
	}
}

// prepare создаёт базы для запуска. Недоступная база — не ошибка запуска:
// переменная окружения не выставляется, а note объясняет ученику, что делать.
func (l *labs) prepare(ctx context.Context, names []string) (env []string, note string, cleanup func()) {
	var drops []func()
	var notes []string
	for _, name := range names {
		admin, ok := l.admin[name]
		if !ok {
			hint := "запустите учебник через docker compose --profile labs up"
			if name == LabKafka {
				hint = "в песочнице Kafka выключена по умолчанию (см. docker-compose.yml), а kfake работает без неё"
			}
			notes = append(notes, fmt.Sprintf("labs: %s в этом runner не настроен — %s", name, hint))
			continue
		}
		l.cleanOnce(name, admin)
		vars, drop, err := l.create(ctx, name, admin)
		if err != nil {
			notes = append(notes, fmt.Sprintf("labs: %s недоступен — запустите docker compose --profile labs up (%v)", name, err))
			continue
		}
		env = append(env, vars...)
		drops = append(drops, drop)
	}
	if len(notes) > 0 {
		note = strings.Join(notes, "\n") + "\n"
	}
	return env, note, func() {
		for _, drop := range drops {
			drop()
		}
	}
}

// create готовит ресурс запуска и возвращает переменные окружения программы.
func (l *labs) create(ctx context.Context, name, admin string) (env []string, drop func(), err error) {
	ctx, cancel := context.WithTimeout(ctx, labTimeout)
	defer cancel()
	id := fmt.Sprintf("%s%08x%s", labPrefix, uint32(time.Now().Unix()), randomHex(4))
	password := randomHex(16)
	var dsn string
	switch name {
	case LabPostgres:
		dsn, err = createPostgres(ctx, admin, id, password)
	case LabMySQL:
		dsn, err = createMySQL(ctx, admin, id, password)
	case LabKafka:
		env, err = createKafka(ctx, admin, id)
	}
	if err != nil {
		return nil, nil, err
	}
	if dsn != "" {
		env = []string{labEnv[name] + "=" + dsn}
	}
	return env, func() {
		// Контекст запроса к этому моменту может быть отменён, а база
		// должна быть удалена всё равно.
		ctx, cancel := context.WithTimeout(context.Background(), labTimeout)
		defer cancel()
		switch name {
		case LabPostgres:
			dropPostgres(ctx, admin, []string{id})
		case LabMySQL:
			dropMySQL(ctx, admin, []string{id})
		case LabKafka:
			dropKafka(ctx, admin, func(owner string) bool { return owner == id })
		}
	}, nil
}

// dropStale удаляет временные базы старше labStaleAge.
func (l *labs) dropStale(name, admin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), labTimeout)
	defer cancel()
	switch name {
	case LabPostgres:
		conn, err := pgx.Connect(ctx, admin)
		if err != nil {
			return err
		}
		ids, err := collectStrings(conn.Query(ctx,
			`SELECT rolname FROM pg_roles WHERE rolname LIKE 'run\_%'
			 UNION SELECT datname FROM pg_database WHERE datname LIKE 'run\_%'`))
		conn.Close(ctx)
		if err != nil {
			return err
		}
		dropPostgres(ctx, admin, staleIDs(ids))
	case LabMySQL:
		db, err := sql.Open("mysql", admin)
		if err != nil {
			return err
		}
		defer db.Close()
		rows, err := db.QueryContext(ctx,
			`SELECT user FROM mysql.user WHERE user LIKE 'run\_%'
			 UNION SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'run\_%'`)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		dropMySQL(ctx, admin, staleIDs(ids))
	case LabKafka:
		return dropKafka(ctx, admin, func(owner string) bool { return len(staleIDs([]string{owner})) == 1 })
	}
	return nil
}

// staleIDs оставляет имена временных баз, созданных раньше labStaleAge.
func staleIDs(ids []string) []string {
	var stale []string
	for _, id := range ids {
		if created, ok := labCreated(id); ok && time.Since(created) > labStaleAge {
			stale = append(stale, id)
		}
	}
	return stale
}

// labCreated — время создания из имени временной базы.
func labCreated(id string) (time.Time, bool) {
	if !validLabID(id) {
		return time.Time{}, false
	}
	sec, err := strconv.ParseUint(id[len(labPrefix):len(labPrefix)+8], 16, 32)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(int64(sec), 0), true
}

func collectStrings(rows pgx.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Имена и пароли — hex из crypto/rand, поэтому их можно вставлять в DDL:
// CREATE ROLE и CREATE DATABASE не принимают параметры запроса.

func createPostgres(ctx context.Context, admin, id, password string) (string, error) {
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)
	for _, q := range []string{
		fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD '%s' CONNECTION LIMIT %d`, id, password, labConnLimit),
		fmt.Sprintf(`CREATE DATABASE %s OWNER %s`, id, id),
		fmt.Sprintf(`REVOKE ALL ON DATABASE %s FROM PUBLIC`, id),
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			dropPostgres(ctx, admin, []string{id})
			return "", err
		}
	}
	u, err := url.Parse(admin)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword(id, password)
	u.Path = "/" + id
	return u.String(), nil
}

func dropPostgres(ctx context.Context, admin string, ids []string) {
	if len(ids) == 0 {
		return
	}
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return
	}
	defer conn.Close(ctx)
	for _, id := range ids {
		if !validLabID(id) {
			continue
		}
		// WITH (FORCE) обрывает соединения, которые программа не закрыла.
		conn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, id))
		conn.Exec(ctx, fmt.Sprintf(`DROP ROLE IF EXISTS %s`, id))
	}
}

func createMySQL(ctx context.Context, admin, id, password string) (string, error) {
	db, err := sql.Open("mysql", admin)
	if err != nil {
		return "", err
	}
	defer db.Close()
	for _, q := range []string{
		fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci", id),
		fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s' WITH MAX_USER_CONNECTIONS %d", id, password, labConnLimit),
		fmt.Sprintf("GRANT ALL ON `%s`.* TO '%s'@'%%'", id, id),
		// Урок о блокировках InnoDB смотрит только эти таблицы; остальная
		// performance_schema показала бы запросы чужих сессий.
		fmt.Sprintf("GRANT SELECT ON performance_schema.data_locks TO '%s'@'%%'", id),
		fmt.Sprintf("GRANT SELECT ON performance_schema.data_lock_waits TO '%s'@'%%'", id),
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			dropMySQL(ctx, admin, []string{id})
			return "", err
		}
	}
	cfg, err := mysql.ParseDSN(admin)
	if err != nil {
		return "", err
	}
	// Программе — только адрес и учётные данные: параметры вроде
	// parseTime урок выставляет сам.
	out := mysql.NewConfig()
	out.Net, out.Addr = cfg.Net, cfg.Addr
	out.User, out.Passwd, out.DBName = id, password, id
	return out.FormatDSN(), nil
}

func dropMySQL(ctx context.Context, admin string, ids []string) {
	if len(ids) == 0 {
		return
	}
	db, err := sql.Open("mysql", admin)
	if err != nil {
		return
	}
	defer db.Close()
	for _, id := range ids {
		if !validLabID(id) {
			continue
		}
		// Соединения программы, которые сервер ещё не заметил закрытыми,
		// держат блокировки метаданных — без KILL DROP DATABASE ждал бы их.
		rows, err := db.QueryContext(ctx, `SELECT id FROM information_schema.processlist WHERE user = ?`, id)
		if err == nil {
			var conns []int64
			for rows.Next() {
				var c int64
				if rows.Scan(&c) == nil {
					conns = append(conns, c)
				}
			}
			rows.Close()
			for _, c := range conns {
				db.ExecContext(ctx, fmt.Sprintf("KILL %d", c))
			}
		}
		// Сначала база, потом пользователь: если DROP DATABASE не пройдёт,
		// уборка найдёт её по пользователю или по имени в следующий раз.
		db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", id))
		db.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", id))
	}
}

func validLabID(id string) bool {
	rest, ok := strings.CutPrefix(id, labPrefix)
	if !ok || len(rest) != 16 {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// HasLab сообщает, настроена ли база профиля labs.
func (e *Executor) HasLab(name string) bool {
	_, ok := e.labs.admin[name]
	return ok
}

// ValidLabs проверяет имена из runner.labs.
func ValidLabs(names []string) error {
	for _, n := range names {
		if !slices.Contains(LabNames, n) {
			return fmt.Errorf("неизвестная база labs %q, допустимы: %s", n, strings.Join(LabNames, ", "))
		}
	}
	return nil
}
