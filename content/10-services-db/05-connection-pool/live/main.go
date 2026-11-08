// Пул из двух соединений и шесть обработчиков. Запустите: итоговый шаг
// перечислит горутины, которые так и не получили соединение, и строку,
// где они ждут. Почините утечку по комментарию ИСПРАВЛЕНИЕ и запустите
// снова — все обработчики завершатся.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// lastOrder читает последний заказ клиента: берёт первую строку и выходит.
// Если заказов больше одного, непрочитанные строки держат соединение,
// а rows никто не закрывает — соединение не вернётся в пул. (Когда Next
// дочитывает до конца и возвращает false, rows закрываются сами, поэтому
// клиенты с одним заказом соединение не теряют.)
func lastOrder(ctx context.Context, db *sql.DB, customer int) (int, error) {
	rows, err := db.QueryContext(ctx, "SELECT id FROM orders WHERE customer = ? ORDER BY id DESC", customer)
	if err != nil {
		return 0, err
	}
	// ИСПРАВЛЕНИЕ: defer rows.Close()
	if !rows.Next() {
		return 0, rows.Err()
	}
	var id int
	err = rows.Scan(&id)
	return id, err // утечка: остальные строки не прочитаны, rows не закрыты
}

func main() {
	db, err := sql.Open("sqlite", "pool.db")
	if err != nil {
		panic(err)
	}
	// db.Close() здесь нет намеренно: он разбудил бы ждущих с ошибкой,
	// а итоговый шаг должен показать их зависшими.
	db.SetMaxOpenConns(2)
	ctx := context.Background()
	db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS orders (id INTEGER PRIMARY KEY, customer INT)")
	// У клиентов 2 и 4 по два заказа, у остальных — по одному.
	db.ExecContext(ctx, "INSERT INTO orders (customer) VALUES (1), (2), (2), (3), (4), (4), (5), (6)")

	var wg sync.WaitGroup
	for customer := 1; customer <= 6; customer++ {
		wg.Add(1)
		go func() { // обработчик запроса
			defer wg.Done()
			id, err := lastOrder(ctx, db, customer)
			fmt.Printf("клиент %d: заказ %d, ошибка %v\n", customer, id, err)
		}()
		time.Sleep(2 * time.Millisecond) // запросы приходят по одному
	}

	// Сервер работает дальше: ждём обработчики не дольше 40 мс.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		fmt.Println("все обработчики завершились")
	case <-time.After(40 * time.Millisecond):
		s := db.Stats()
		fmt.Printf("пул: открыто %d, занято %d, ждали соединения %d раз\n", s.OpenConnections, s.InUse, s.WaitCount)
	}
}
