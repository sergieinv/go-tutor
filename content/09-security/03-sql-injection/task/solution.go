package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Schema — таблица товаров. Её создают тесты.
const Schema = `CREATE TABLE products (
	id       INTEGER PRIMARY KEY,
	name     TEXT NOT NULL,
	category TEXT NOT NULL,
	price    INTEGER NOT NULL
)`

// Product — строка таблицы products.
type Product struct {
	ID       int64
	Name     string
	Category string
	Price    int64
}

// Filter — параметры поиска, все приходят от клиента как есть.
type Filter struct {
	NameContains string   // подстрока имени; "" — без фильтра
	Categories   []string // категория — одна из списка; пусто — без фильтра
	Sort         string   // "", "id", "-id", "name", "-name", "price", "-price"
}

var ErrBadSort = errors.New("bad sort")

// orderBy — allowlist: ввод выбирает готовый фрагмент SQL, но не пишет его.
var orderBy = map[string]string{
	"":       "id",
	"id":     "id",
	"-id":    "id DESC",
	"name":   "name, id",
	"-name":  "name DESC, id",
	"price":  "price, id",
	"-price": "price DESC, id",
}

// likeEscaper делает %, _ и сам \ обычными символами в шаблоне LIKE ... ESCAPE '\'.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Search возвращает товары, подходящие под фильтр, в заданном порядке.
func Search(ctx context.Context, db *sql.DB, f Filter) ([]Product, error) {
	order, ok := orderBy[f.Sort]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrBadSort, f.Sort)
	}

	// В текст запроса попадают только константы и знаки ?;
	// всё, что пришло от клиента, — в args.
	var where []string
	var args []any
	if f.NameContains != "" {
		where = append(where, `name LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscaper.Replace(f.NameContains)+"%")
	}
	if len(f.Categories) > 0 {
		where = append(where, "category IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.Categories)), ",")+")")
		for _, c := range f.Categories {
			args = append(args, c)
		}
	}
	q := "SELECT id, name, category, price FROM products"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY " + order

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Category, &p.Price); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
