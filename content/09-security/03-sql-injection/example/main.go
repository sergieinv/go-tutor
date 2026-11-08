// SQL-инъекция — это ввод, ставший частью текста запроса.
// Плейсхолдер держит их порознь: драйвер получает текст с `?`
// и значения отдельно. Функция `query` печатает, что именно
// уходит драйверу.

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	_ "modernc.org/sqlite"
)

func query(ctx context.Context, db *sql.DB, q string, args ...any) {
	shown := make([]string, len(args))
	for i, a := range args {
		shown[i] = fmt.Sprintf("%#v", a)
	}
	fmt.Printf("  SQL:  %s\n  args: [%s]\n", q, strings.Join(shown, ", "))
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		fmt.Println("  ошибка:", err)
		return
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			log.Fatal(err)
		}
		names = append(names, n)
	}
	fmt.Printf("  → строк: %d %q\n", len(names), names)
}

// УЯЗВИМО: ввод вклеен в текст SQL. Кавычка из ввода закрывает
// литерал, остаток разбирается как SQL — условие становится
// истинным для всех строк.
func findUserUnsafe(ctx context.Context, db *sql.DB, name string) {
	query(ctx, db, "SELECT name FROM users WHERE name = '"+name+"'")
}

// Плейсхолдер: текст запроса постоянный, значение никогда не
// разбирается как SQL — это просто строка для сравнения.
func findUser(ctx context.Context, db *sql.DB, name string) {
	query(ctx, db, "SELECT name FROM users WHERE name = ?", name)
}

// Имя колонки и направление сортировки плейсхолдером не
// передать — это часть структуры запроса. Только allowlist:
// ввод выбирает готовый фрагмент SQL, но не пишет его.
var sortSQL = map[string]string{"name": "name", "-name": "name DESC", "age": "age", "-age": "age DESC"}

func listUsers(ctx context.Context, db *sql.DB, sort string) {
	col, ok := sortSQL[sort]
	if !ok {
		fmt.Printf("  сортировка %q не из списка — отказ\n", sort)
		return
	}
	query(ctx, db, "SELECT name FROM users ORDER BY "+col+", id")
}

// В `LIKE` свои метасимволы: `%` и `_` из ввода превратят поиск
// подстроки в «всё подряд». Экранируем их и `\` и объявляем
// `ESCAPE`. Это уже не защита от инъекции, а точность поиска.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func searchUsers(ctx context.Context, db *sql.DB, sub string) {
	query(ctx, db, `SELECT name FROM users WHERE name LIKE ? ESCAPE '\' ORDER BY id`, "%"+likeEscaper.Replace(sub)+"%")
}

// `IN (...)` со списком: столько `?`, сколько значений.
// Пустой список — отдельный случай: `IN ()` — синтаксическая ошибка.
func usersByID(ctx context.Context, db *sql.DB, ids []int64) {
	if len(ids) == 0 {
		fmt.Println("  пустой список — запрос не нужен")
		return
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	query(ctx, db, "SELECT name FROM users WHERE id IN ("+ph+") ORDER BY id", args...)
}

func main() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // у каждого соединения с :memory: своя база
	_, err = db.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER);
		INSERT INTO users (name, age) VALUES ('alice', 31), ('bob', 25), ('100%_real', 40)`)
	if err != nil {
		log.Fatal(err)
	}

	evil := "' OR 1=1 --"
	fmt.Println("склейка:")
	findUserUnsafe(ctx, db, evil)
	fmt.Println("плейсхолдер:")
	findUser(ctx, db, evil)
	fmt.Println("сортировка:")
	listUsers(ctx, db, "-age")
	listUsers(ctx, db, "age; DROP TABLE users")
	fmt.Println("LIKE:")
	searchUsers(ctx, db, "%_")
	fmt.Println("IN:")
	usersByID(ctx, db, []int64{3, 1})
}
