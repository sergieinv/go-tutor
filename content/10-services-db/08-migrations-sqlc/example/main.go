// Схема живёт в SQL-миграциях goose (`00001_*.sql`,
// `00002_*.sql`), запросы — в `query.sql`. По ним
// `sqlc generate` (настройки — `sqlc.yaml`) создал
// `db.go`, `models.go` и `query.sql.go`: типы строк и
// методы с плейсхолдерами. Их не правят руками.

package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// Миграции встраиваются в бинарник: на сервере не нужен
// каталог с файлами. Шаблон берёт только `0*.sql` —
// `query.sql` не миграция.
//
//go:embed 0*.sql
var migrations embed.FS

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "shop")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "shop.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Провайдер goose: диалект SQL, пул и файловая система
	// с миграциями. Конструктор только читает список файлов.
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations)
	if err != nil {
		log.Fatal(err)
	}

	// `Up` создаёт таблицу версий `goose_db_version` и
	// применяет все ожидающие миграции — каждую в своей
	// транзакции. Повторный `Up` ничего не делает.
	res, err := provider.Up(ctx)
	report("up", res, err)
	res, err = provider.Up(ctx)
	report("повторный up", res, err)

	// Сгенерированный код: `New` принимает `DBTX` — его
	// реализуют и `*sql.DB`, и `*sql.Tx`. Параметры — поля
	// структуры, `NULL`-столбец — `sql.NullString`.
	q := New(db)
	tea, err := q.CreateProduct(ctx, CreateProductParams{
		Name: "чай", Price: 300, Note: sql.NullString{String: "улун", Valid: true}, Stock: 5,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("создан %+v\n", tea)
	if _, err := q.CreateProduct(ctx, CreateProductParams{Name: "кофе", Price: 450}); err != nil {
		log.Fatal(err)
	}

	// Транзакция передаётся явно: `q.WithTx(tx)` — те же
	// методы поверх `*sql.Tx`.
	if err := addStock(ctx, db, q, 2, 10); err != nil {
		log.Fatal(err)
	}
	rows, err := q.ListInStock(ctx, 1)
	fmt.Println("в наличии:", rows, err)

	// `:one` возвращает `sql.ErrNoRows`, как `Scan`.
	_, err = q.GetProduct(ctx, 42)
	fmt.Println("id 42:", errors.Is(err, sql.ErrNoRows))

	// Откат до версии 1 выполняет `Down` миграции 2:
	// столбца `stock` больше нет — и код, сгенерированный
	// под новую схему, ломается уже во время выполнения.
	res, err = provider.DownTo(ctx, 1)
	report("down до 1", res, err)
	_, err = q.ListInStock(ctx, 1)
	fmt.Println("ListInStock:", err)

	// Снова `Up`: столбец вернулся, но со значением по
	// умолчанию — `Down` уничтожил данные.
	res, err = provider.Up(ctx)
	report("up", res, err)
	rows, err = q.ListInStock(ctx, 0)
	fmt.Println("остатки:", rows, err)
	version, err := provider.GetDBVersion(ctx)
	fmt.Println("версия схемы:", version, err)
}

// addStock пополняет склад в транзакции: при любой ошибке
// `defer tx.Rollback()` откатит изменения.
func addStock(ctx context.Context, db *sql.DB, q *Queries, id, delta int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	n, err := q.WithTx(tx).Restock(ctx, RestockParams{Delta: delta, ID: id})
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("товар %d не найден", id)
	}
	return tx.Commit()
}

// report печатает применённые миграции без длительностей:
// `MigrationResult.String()` включает время выполнения.
func report(step string, res []*goose.MigrationResult, err error) {
	fmt.Printf("%s: %d миграций, err=%v\n", step, len(res), err)
	for _, r := range res {
		fmt.Printf("  %-4s %s\n", r.Direction, filepath.Base(r.Source.Path))
	}
}
