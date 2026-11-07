package task

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func balances(t *testing.T, repo *Repo, ids ...int64) []int64 {
	t.Helper()
	out := make([]int64, len(ids))
	for i, id := range ids {
		a, err := repo.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get(%d) = %v", id, err)
		}
		out[i] = a.Balance
	}
	return out
}

func TestNullEmail(t *testing.T) {
	repo, db := newRepo(t)
	ctx := context.Background()
	id, err := repo.Create(ctx, "Борис", nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	var isNull bool
	if err := db.QueryRow(`SELECT email IS NULL FROM accounts WHERE id = ?`, id).Scan(&isNull); err != nil || !isNull {
		t.Fatalf("email = nil должен храниться как NULL (isNull=%v, err=%v)", isNull, err)
	}
	a, err := repo.Get(ctx, id)
	if err != nil || a.Email != nil {
		t.Fatalf("Get: Email = %v, err = %v; want nil", a.Email, err)
	}
	id2, _ := repo.Create(ctx, "Вера", ptr(""), 5)
	a, err = repo.Get(ctx, id2)
	if err != nil || a.Email == nil || *a.Email != "" {
		t.Fatalf("пустая строка — не NULL: Email = %v, err = %v", a.Email, err)
	}
}

func TestCreateSQLInjection(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	evil := `x', 'e', 0); DROP TABLE accounts; --`
	id, err := repo.Create(ctx, evil, ptr(`'; DELETE FROM accounts; --`), 1)
	if err != nil {
		t.Fatalf("Create = %v: значения передаются плейсхолдерами, кавычки — не синтаксис", err)
	}
	a, err := repo.Get(ctx, id)
	if err != nil || a.Owner != evil || *a.Email != `'; DELETE FROM accounts; --` {
		t.Fatalf("Get = (%+v, %v): строка должна сохраниться как есть", a, err)
	}
}

func TestCreateIDsAndNegative(t *testing.T) {
	repo, db := newRepo(t)
	ctx := context.Background()
	for want := int64(1); want <= 3; want++ {
		if id, err := repo.Create(ctx, "o", nil, 0); err != nil || id != want {
			t.Fatalf("Create = (%d, %v), want %d", id, err, want)
		}
	}
	if _, err := repo.Create(ctx, "neg", nil, -1); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("Create с отрицательным балансом = %v, want ErrInvalidAmount", err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n)
	if n != 3 {
		t.Fatalf("в таблице %d строк, want 3", n)
	}
}

func TestGetNotFoundZeroValue(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	repo.Create(ctx, "a", ptr("a@x"), 10)
	a, err := repo.Get(ctx, 2)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(2) = %v, want ErrNotFound", err)
	}
	if a.ID != 0 || a.Owner != "" || a.Email != nil || a.Balance != 0 {
		t.Fatalf("при ошибке Get вернул %+v, want Account{}", a)
	}
}

func TestList(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	list, err := repo.List(ctx, 0)
	if err != nil || len(list) != 0 {
		t.Fatalf("List пустой таблицы = (%v, %v), want пусто", list, err)
	}
	for i, bal := range []int64{50, 10, 70, 30} {
		repo.Create(ctx, string(rune('a'+i)), nil, bal)
	}
	list, err = repo.List(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, a := range list {
		got = append(got, a.ID)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 4 {
		t.Fatalf("List(30) вернул id %v, want [1 3 4] (balance >= 30, по возрастанию id)", got)
	}
	if list[1].Owner != "c" || list[1].Balance != 70 || list[1].Email != nil {
		t.Fatalf("List: %+v", list[1])
	}
	if list, _ := repo.List(ctx, 1000); len(list) != 0 {
		t.Fatalf("List(1000) = %v, want пусто", list)
	}
}

func TestTransferErrors(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	a, _ := repo.Create(ctx, "a", nil, 50)
	b, _ := repo.Create(ctx, "b", nil, 0)

	tests := []struct {
		name          string
		from, to, sum int64
		want          error
	}{
		{"zero", a, b, 0, ErrInvalidAmount},
		{"negative", a, b, -10, ErrInvalidAmount},
		{"self", a, a, 10, ErrInvalidAmount},
		{"no from", 99, b, 10, ErrNotFound},
		{"no to", a, 99, 10, ErrNotFound},
		{"no to, too much", a, 99, 1000, ErrNotFound},
		{"too much", a, b, 51, ErrInsufficientFunds},
		{"from empty", b, a, 1, ErrInsufficientFunds},
	}
	for _, tt := range tests {
		if err := repo.Transfer(ctx, tt.from, tt.to, tt.sum); !errors.Is(err, tt.want) {
			t.Errorf("Transfer(%s) = %v, want %v", tt.name, err, tt.want)
		}
	}
	if got := balances(t, repo, a, b); got[0] != 50 || got[1] != 0 {
		t.Fatalf("после неудачных переводов балансы %v, want [50 0]", got)
	}
	if err := repo.Transfer(ctx, a, b, 50); err != nil {
		t.Fatalf("перевод всего баланса = %v", err)
	}
	if got := balances(t, repo, a, b); got[0] != 0 || got[1] != 50 {
		t.Fatalf("балансы %v, want [0 50]", got)
	}
}

func TestTransferReleasesConnection(t *testing.T) {
	// Пул из одного соединения: незавершённая транзакция или
	// незакрытые rows заблокируют следующий запрос.
	repo, db := newRepo(t)
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	a, _ := repo.Create(ctx, "a", nil, 10)
	b, _ := repo.Create(ctx, "b", nil, 0)
	for _, sum := range []int64{100, 5} {
		repo.Transfer(ctx, a, b, sum)
		repo.Transfer(ctx, a, 99, 1)
		repo.List(ctx, 0)
		tctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := repo.Get(tctx, a)
		cancel()
		if err != nil {
			t.Fatalf("Get после Transfer/List = %v: соединение не вернулось в пул (нет Rollback/Commit или rows.Close?)", err)
		}
	}
}

func TestTransferConcurrent(t *testing.T) {
	repo, db := newRepo(t)
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	a, _ := repo.Create(ctx, "a", nil, 10)
	b, _ := repo.Create(ctx, "b", nil, 0)

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, insufficient := 0, 0
	for range 20 {
		wg.Go(func() {
			err := repo.Transfer(ctx, a, b, 1)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrInsufficientFunds):
				insufficient++
			default:
				t.Errorf("Transfer = %v", err)
			}
		})
	}
	wg.Wait()
	if ok != 10 || insufficient != 10 {
		t.Errorf("успешных переводов %d, отказов %d; want 10 и 10: проверка и списание — в одной транзакции", ok, insufficient)
	}
	if got := balances(t, repo, a, b); got[0] != 0 || got[1] != 10 {
		t.Fatalf("балансы %v, want [0 10]", got)
	}
}

func TestCanceledContext(t *testing.T) {
	repo, _ := newRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	id, _ := repo.Create(context.Background(), "a", nil, 10)
	cancel()
	if _, err := repo.Get(ctx, id); !errors.Is(err, context.Canceled) {
		t.Errorf("Get с отменённым ctx = %v, want context.Canceled", err)
	}
	if _, err := repo.Create(ctx, "b", nil, 1); !errors.Is(err, context.Canceled) {
		t.Errorf("Create с отменённым ctx = %v, want context.Canceled", err)
	}
	if _, err := repo.List(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("List с отменённым ctx = %v, want context.Canceled", err)
	}
	if err := repo.Transfer(ctx, id, id+1, 1); err == nil {
		t.Errorf("Transfer с отменённым ctx = nil, want ошибку")
	}
}
