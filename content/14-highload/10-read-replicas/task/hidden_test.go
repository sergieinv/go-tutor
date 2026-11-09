package task

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestParseLSNEdges(t *testing.T) {
	good := map[string]LSN{
		"16/b374d848":       0x16_B374D848,
		"FFFFFFFF/FFFFFFFF": 0xFFFFFFFF_FFFFFFFF,
		"00000000/00000001": 1,
		"a/0":               0xA << 32,
	}
	for in, want := range good {
		got, err := ParseLSN(in)
		if err != nil || got != want {
			t.Errorf("ParseLSN(%q) = %X, %v; want %X", in, uint64(got), err, uint64(want))
		}
	}
	bad := []string{
		"100000000/0", "0/123456789", "1/2/3", "+1/2", "-1/2", " 1/2", "1/2 ",
		"0x1/2", "1 /2", "g/1", "1/g", "/", "1\n/2", "１/2",
	}
	for _, in := range bad {
		if got, err := ParseLSN(in); !errors.Is(err, ErrBadLSN) {
			t.Errorf("ParseLSN(%q) = %X, %v; want ErrBadLSN", in, uint64(got), err)
		}
	}
}

// Сравнивать LSN строками нельзя: "0/A" > "0/10" как строки, но
// позиция 0xA меньше 0x10.
func TestLSNOrderIsNumeric(t *testing.T) {
	a, err1 := ParseLSN("0/A")
	b, err2 := ParseLSN("0/10")
	c, err3 := ParseLSN("1/0")
	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatal(err1, err2, err3)
	}
	if !(a < b && b < c) {
		t.Fatalf("0/A=%d, 0/10=%d, 1/0=%d — ожидался рост", uint64(a), uint64(b), uint64(c))
	}
}

func TestLSNStringRoundTrip(t *testing.T) {
	for _, l := range []LSN{0, 1, 0xA, 0x1_00000000, 0xFFFFFFFF_FFFFFFFF, 0x3_0000ABCD} {
		s := l.String()
		back, err := ParseLSN(s)
		if err != nil || back != l {
			t.Errorf("%X: String %q, обратно %X, %v", uint64(l), s, uint64(back), err)
		}
	}
	if got := LSN(0).String(); got != "0/0" {
		t.Errorf("LSN(0) = %q, want 0/0", got)
	}
	if got := LSN(0x3_0000ABCD).String(); got != "3/ABCD" {
		t.Errorf("String = %q, want 3/ABCD (без ведущих нулей, заглавные)", got)
	}
}

// served — какая реплика ответила (индекс), или -1 для основного.
func served(t *testing.T, r *Router, token LSN, reps []*fakeDB, primary *fakeDB) int {
	t.Helper()
	before := make([]int, len(reps))
	for i, rep := range reps {
		before[i], _ = rep.calls()
	}
	pBefore, _ := primary.calls()
	_, fromReplica, err := r.History(context.Background(), 7, token)
	if err != nil {
		t.Fatal(err)
	}
	if !fromReplica {
		if p, _ := primary.calls(); p != pBefore+1 {
			t.Fatalf("fromReplica = false, а основной вызван %d раз", p-pBefore)
		}
		return -1
	}
	who := -1
	for i, rep := range reps {
		h, _ := rep.calls()
		if h > before[i] && rep.err == nil {
			who = i
		}
	}
	return who
}

func TestRoundRobin(t *testing.T) {
	primary := &fakeDB{}
	reps := []*fakeDB{{lsn: 10}, {lsn: 10}, {lsn: 10}}
	r := NewRouter(primary, reps[0], reps[1], reps[2])
	var got []int
	for range 6 {
		got = append(got, served(t, r, 0, reps, primary))
	}
	want := []int{0, 1, 2, 0, 1, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ответившие реплики = %v, want %v", got, want)
		}
	}
	for i, rep := range reps {
		if _, replay := rep.calls(); replay != 0 {
			t.Errorf("реплика %d: ReplayLSN вызван %d раз при нулевом токене, want 0", i, replay)
		}
	}
}

func TestSkipLaggingAndBroken(t *testing.T) {
	primary := &fakeDB{}
	reps := []*fakeDB{
		{lsn: 50},                           // отстаёт
		{lsn: 900, lsnErr: errors.New("x")}, // не отвечает на ReplayLSN
		{lsn: 100},                          // ровно догнала
	}
	r := NewRouter(primary, reps[0], reps[1], reps[2])
	if who := served(t, r, 100, reps, primary); who != 2 {
		t.Fatalf("ответила %d, want 2 (LSN равен токену — подходит)", who)
	}
	if h, _ := reps[0].calls(); h != 0 {
		t.Fatalf("History отстающей реплики вызван %d раз", h)
	}
	if h, _ := reps[1].calls(); h != 0 {
		t.Fatalf("History реплики с ошибкой ReplayLSN вызван %d раз", h)
	}
	// Следующий вызов начинает с реплики 1, обходит 2 — она подходит.
	if who := served(t, r, 100, reps, primary); who != 2 {
		t.Fatalf("второй вызов: ответила %d, want 2", who)
	}
	// Токен новее всех — основной.
	if who := served(t, r, 101, reps, primary); who != -1 {
		t.Fatalf("токен 101: ответила %d, want основной", who)
	}
}

func TestReplicaHistoryErrorFallsThrough(t *testing.T) {
	primary := &fakeDB{orders: []Order{{1, 1}}}
	boom := errors.New("replica down")
	reps := []*fakeDB{{lsn: 10, err: boom}, {lsn: 10, err: boom}, {lsn: 10, err: boom}}
	r := NewRouter(primary, reps[0], reps[1], reps[2])
	orders, fromReplica, err := r.History(context.Background(), 7, 5)
	if err != nil || fromReplica || len(orders) != 1 {
		t.Fatalf("History = %v, %v, %v; want заказ с основного", orders, fromReplica, err)
	}
	for i, rep := range reps {
		if h, replay := rep.calls(); h != 1 || replay != 1 {
			t.Errorf("реплика %d: History %d, ReplayLSN %d — каждую пробуют ровно раз", i, h, replay)
		}
	}
	if p, _ := primary.calls(); p != 1 {
		t.Fatalf("основной вызван %d раз, want 1", p)
	}
}

func TestPrimaryError(t *testing.T) {
	boom := errors.New("primary down")
	primary := &fakeDB{err: boom}
	r := NewRouter(primary, &fakeDB{lsn: 1})
	_, fromReplica, err := r.History(context.Background(), 7, 2)
	if !errors.Is(err, boom) || fromReplica {
		t.Fatalf("err = %v, fromReplica %v; want ошибку основного", err, fromReplica)
	}
}

func TestCanceledBefore(t *testing.T) {
	primary := &fakeDB{}
	rep := &fakeDB{lsn: 10}
	r := NewRouter(primary, rep)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := r.History(ctx, 7, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	h, replay := rep.calls()
	p, _ := primary.calls()
	if h+replay+p != 0 {
		t.Fatalf("с отменённым контекстом обращений: реплика %d+%d, основной %d", h, replay, p)
	}
}

// Контекст отменили, пока шёл запрос к реплике: роутер не идёт ни
// на следующую реплику, ни на основной.
func TestCanceledDuringReplica(t *testing.T) {
	for _, token := range []LSN{0, 5} {
		ctx, cancel := context.WithCancel(context.Background())
		primary := &fakeDB{}
		first := &fakeDB{lsn: 10, err: context.Canceled, onCall: cancel}
		if token > 0 {
			first = &fakeDB{lsn: 10, lsnErr: context.Canceled, onCall: cancel}
		}
		second := &fakeDB{lsn: 10}
		r := NewRouter(primary, first, second)
		if _, _, err := r.History(ctx, 7, token); !errors.Is(err, context.Canceled) {
			t.Fatalf("token %d: err = %v, want context.Canceled", token, err)
		}
		h, replay := second.calls()
		p, _ := primary.calls()
		if h+replay+p != 0 {
			t.Fatalf("token %d: после отмены обращений: вторая реплика %d+%d, основной %d", token, h, replay, p)
		}
	}
}

// Роутер общий для всех обработчиков: под -race без гонок, а
// круговой обход раздаёт вызовы поровну.
func TestConcurrent(t *testing.T) {
	primary := &fakeDB{}
	reps := []*fakeDB{{lsn: 10}, {lsn: 10}, {lsn: 10}}
	r := NewRouter(primary, reps[0], reps[1], reps[2])
	var wg sync.WaitGroup
	for range 300 {
		wg.Go(func() {
			if _, ok, err := r.History(context.Background(), 7, 0); err != nil || !ok {
				t.Errorf("History: %v, %v", ok, err)
			}
		})
	}
	wg.Wait()
	for i, rep := range reps {
		if h, _ := rep.calls(); h != 100 {
			t.Errorf("реплика %d: вызовов %d, want 100", i, h)
		}
	}
}
