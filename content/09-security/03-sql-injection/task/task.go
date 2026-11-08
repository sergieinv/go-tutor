package task

import (
	"context"
	"database/sql"
	"errors"
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

// Search возвращает товары, подходящие под фильтр, в заданном порядке.
func Search(ctx context.Context, db *sql.DB, f Filter) ([]Product, error) {
	// TODO: реализуйте функцию
	return nil, nil
}
