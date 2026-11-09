package task

import (
	"context"
	"errors"
	"testing"
)

func TestCompensatedRerunDoesNothing(t *testing.T) {
	store := newStore()
	j := &journal{fail: map[string]error{"do:order": errors.New("нет")}}
	Run(context.Background(), store, "s", j.steps("reserve", "order", "pay"))
	j.calls = nil
	saves := store.saves
	st, err := Run(context.Background(), store, "s", j.steps("reserve", "order", "pay"))
	if st != StatusCompensated || err != nil || len(j.calls) != 0 || store.saves != saves {
		t.Fatalf("повтор: %q, %v, вызовы %v, сохранений +%d; want compensated, nil, ничего", st, err, j.calls, store.saves-saves)
	}
}

func TestFirstStepFails(t *testing.T) {
	errNo := errors.New("нет товара")
	store := newStore()
	j := &journal{fail: map[string]error{"do:reserve": errNo}}
	st, err := Run(context.Background(), store, "s", j.steps("reserve", "order"))
	if st != StatusCompensated || !errors.Is(err, errNo) {
		t.Fatalf("Run = %q, %v", st, err)
	}
	eq(t, "вызовы", j.calls, []string{"do:reserve"}) // упавший шаг не компенсируется
}

func TestNilCompensateSkipped(t *testing.T) {
	store := newStore()
	j := &journal{fail: map[string]error{"do:pay": errors.New("отказ")}}
	steps := j.steps("reserve", "notify", "pay")
	steps[1].Compensate = nil
	st, _ := Run(context.Background(), store, "s", steps)
	if st != StatusCompensated {
		t.Fatalf("Run = %q", st)
	}
	eq(t, "вызовы", j.calls, []string{"do:reserve", "do:notify", "do:pay", "undo:reserve"})
}

func TestCompensationFailureThenResume(t *testing.T) {
	errDeclined := errors.New("карта отклонена")
	errOrders := errors.New("заказы недоступны")
	store := newStore()
	j := &journal{fail: map[string]error{"do:pay": errDeclined, "undo:order": errOrders}}
	st, err := Run(context.Background(), store, "s", j.steps("reserve", "order", "pay"))
	if st != StatusFailed || !errors.Is(err, errOrders) {
		t.Fatalf("Run = %q, %v; want failed и ошибку компенсации", st, err)
	}
	eq(t, "вызовы", j.calls, []string{"do:reserve", "do:order", "do:pay", "undo:order"})
	eq(t, "состояние", store.states["s"].Status, StatusFailed)
	eq(t, "невыполненные компенсации", store.states["s"].Done, []string{"reserve", "order"})

	// Сервис заказов ожил — повторный запуск дочищает компенсации,
	// шаги вперёд не выполняются.
	j.calls = nil
	delete(j.fail, "undo:order")
	st, err = Run(context.Background(), store, "s", j.steps("reserve", "order", "pay"))
	if st != StatusCompensated || err != nil {
		t.Fatalf("повтор: %q, %v; want compensated, nil", st, err)
	}
	eq(t, "вызовы при повторе", j.calls, []string{"undo:order", "undo:reserve"})
}

func TestCrashDuringForwardResumes(t *testing.T) {
	// Сохранение после шага order не удалось — «процесс упал».
	store := newStore()
	store.failSave = 2
	j := &journal{}
	st, err := Run(context.Background(), store, "s", j.steps("reserve", "order", "pay"))
	if st != "" || !errors.Is(err, errSave) {
		t.Fatalf("Run = %q, %v; want \"\" и ошибку сохранения", st, err)
	}
	eq(t, "вызовы до сбоя", j.calls, []string{"do:reserve", "do:order"})
	eq(t, "сохранено", store.states["s"].Done, []string{"reserve"})

	// Перезапуск: reserve пропущен, order повторён (поэтому шаги
	// идемпотентны), pay выполнен.
	j.calls = nil
	store.failSave = 0
	st, err = Run(context.Background(), store, "s", j.steps("reserve", "order", "pay"))
	if st != StatusCompleted || err != nil {
		t.Fatalf("перезапуск: %q, %v", st, err)
	}
	eq(t, "вызовы после перезапуска", j.calls, []string{"do:order", "do:pay"})
}

func TestCrashDuringCompensationResumes(t *testing.T) {
	store := newStore()
	j := &journal{fail: map[string]error{"do:pay": errors.New("отказ")}}
	// Сохранения: reserve(1), order(2), compensating(3), после undo:order(4) — падает.
	store.failSave = 4
	st, err := Run(context.Background(), store, "s", j.steps("reserve", "order", "pay"))
	if st != "" || !errors.Is(err, errSave) {
		t.Fatalf("Run = %q, %v; want \"\" и ошибку сохранения", st, err)
	}
	eq(t, "вызовы до сбоя", j.calls, []string{"do:reserve", "do:order", "do:pay", "undo:order"})
	eq(t, "статус", store.states["s"].Status, StatusCompensating)

	j.calls = nil
	store.failSave = 0
	st, err = Run(context.Background(), store, "s", j.steps("reserve", "order", "pay"))
	if st != StatusCompensated || err != nil {
		t.Fatalf("перезапуск: %q, %v", st, err)
	}
	eq(t, "вызовы после перезапуска", j.calls, []string{"undo:order", "undo:reserve"})
}

func TestLoadError(t *testing.T) {
	store := newStore()
	store.loadErr = errSave
	j := &journal{}
	st, err := Run(context.Background(), store, "s", j.steps("reserve"))
	if st != "" || !errors.Is(err, errSave) || len(j.calls) != 0 {
		t.Fatalf("Run = %q, %v, вызовы %v", st, err, j.calls)
	}
}

func TestSagasIndependent(t *testing.T) {
	store := newStore()
	j := &journal{}
	Run(context.Background(), store, "a", j.steps("reserve"))
	j.calls = nil
	st, _ := Run(context.Background(), store, "b", j.steps("reserve"))
	if st != StatusCompleted || len(j.calls) != 1 {
		t.Fatalf("сага b: %q, вызовы %v — состояние у каждой саги своё", st, j.calls)
	}
}

func TestUnknownStatus(t *testing.T) {
	store := newStore()
	store.states["s"] = State{Status: "paused", Done: []string{"reserve"}}
	j := &journal{}
	st, err := Run(context.Background(), store, "s", j.steps("reserve", "order"))
	if st != "" || !errors.Is(err, ErrUnknownStatus) {
		t.Fatalf("Run = %q, %v; want \"\" и ErrUnknownStatus", st, err)
	}
	if len(j.calls) != 0 || store.saves != 0 {
		t.Errorf("вызовы %v, сохранений %d; want ничего — неизвестный статус не запускает откат", j.calls, store.saves)
	}
}
