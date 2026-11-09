package task

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestHiddenErrors(t *testing.T) {
	h := NewHub()
	mc := &binProto{name: "mc"}
	if err := h.Add(-3, mc); !errors.Is(err, ErrBadPlayer) {
		t.Errorf("Add(-3) = %v, want ErrBadPlayer", err)
	}
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 2, mc)
	if err := h.SetVisible(1, 1, true); !errors.Is(err, ErrBadPlayer) {
		t.Errorf("SetVisible(1, 1) = %v, want ErrBadPlayer", err)
	}
	if err := h.SetVisible(7, 7, true); !errors.Is(err, ErrBadPlayer) {
		t.Errorf("SetVisible(7, 7) = %v, want ErrBadPlayer (проверка a == b — первой)", err)
	}
	if err := h.SetVisible(9, 1, false); !errors.Is(err, ErrUnknownPlayer) {
		t.Errorf("SetVisible(9, 1, false) = %v, want ErrUnknownPlayer", err)
	}
	if err := h.SetVisible(1, 2, false); err != nil {
		t.Errorf("SetVisible(1, 2, false) без пары = %v, want nil", err)
	}
	if h.Remove(9) {
		t.Error("Remove(9) = true для неизвестного игрока")
	}
	if !h.Remove(2) || h.Remove(2) {
		t.Error("Remove(2): первый вызов — true, второй — false")
	}
}

func TestHiddenSymmetricVisibility(t *testing.T) {
	h := NewHub()
	mc := &binProto{name: "mc"}
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 2, mc)
	mustAdd(t, h, 3, mc)
	mustSee(t, h, 2, 1) // пара в обе стороны, в каком порядке ни задай
	mustSee(t, h, 3, 1)
	h.Emit(Item{Event: Event{Kind: "a", Subject: 1}, To: ToViewers})
	h.Emit(Item{Event: Event{Kind: "b", Subject: 2}, To: ToViewers})
	if got, want := render(h.Flush(1)), "1=mc:b:2:; 2=mc:a:1:; 3=mc:a:1:;"; got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
	if err := h.SetVisible(1, 3, false); err != nil {
		t.Fatal(err)
	}
	h.Emit(Item{Event: Event{Kind: "c", Subject: 3}, To: ToViewers})
	h.Emit(Item{Event: Event{Kind: "d", Subject: 1}, To: ToViewers})
	if got, want := render(h.Flush(2)), "2=mc:d:1:;"; got != want {
		t.Errorf("после SetVisible(1, 3, false):\n got %s\nwant %s", got, want)
	}
}

func TestHiddenRemove(t *testing.T) {
	h := NewHub()
	mc, web := &binProto{name: "mc"}, &jsonProto{}
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 2, mc)
	mustAdd(t, h, 3, web)
	mustSee(t, h, 1, 2)
	mustSee(t, h, 1, 3)
	// События, отправленные до Remove, адресуются на момент Flush.
	h.Emit(Item{Event: Event{Kind: "move", Subject: 1}, To: ToViewers, Self: true})
	h.Emit(Item{Event: Event{Kind: "welcome", Subject: 1}, To: ToOne, Target: 1})
	h.Emit(Item{Event: Event{Kind: "chat", Subject: 2}, To: ToViewers, Self: true})
	h.Remove(1)
	got := render(h.Flush(3))
	want := `2=mc:chat:2:; 3=[{"t":"tick","n":3,"msgs":0}]`
	if got != want {
		t.Errorf("Flush после Remove(1):\n got %s\nwant %s", got, want)
	}
	if mc.appends != 1 || web.appends != 0 {
		t.Errorf("Append: mc=%d web=%d; для ушедших игроков кодировать нечего", mc.appends, web.appends)
	}
	// Вернулся с тем же id — видимость не сохранилась.
	mustAdd(t, h, 1, web)
	h.Emit(Item{Event: Event{Kind: "x", Subject: 1}, To: ToViewers})
	h.Emit(Item{Event: Event{Kind: "y", Subject: 2}, To: ToViewers})
	got = render(h.Flush(4))
	want = `1=[{"t":"tick","n":4,"msgs":0}] 3=[{"t":"tick","n":4,"msgs":0}]`
	if got != want {
		t.Errorf("повторный вход:\n got %s\nwant %s", got, want)
	}
}

func TestHiddenExclude(t *testing.T) {
	h := NewHub()
	mc := &binProto{name: "mc"}
	for id := PlayerID(1); id <= 3; id++ {
		mustAdd(t, h, id, mc)
	}
	h.Emit(Item{Event: Event{Kind: "e2"}, To: ToAll, Exclude: 2})
	h.Emit(Item{Event: Event{Kind: "e0"}, To: ToAll})
	h.Emit(Item{Event: Event{Kind: "e9"}, To: ToAll, Exclude: 9})
	got := render(h.Flush(1))
	want := "1=mc:e2:0:;mc:e0:0:;mc:e9:0:; 2=mc:e0:0:;mc:e9:0:; 3=mc:e2:0:;mc:e0:0:;mc:e9:0:;"
	if got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
	if mc.appends != 3 {
		t.Errorf("Append вызван %d раз, want 3 (по разу на событие)", mc.appends)
	}
}

func TestHiddenNoRecipientsNoEncode(t *testing.T) {
	h := NewHub()
	mc, web := &binProto{name: "mc"}, &jsonProto{}
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 2, mc)
	mustAdd(t, h, 3, web)
	mustSee(t, h, 1, 2)
	h.Emit(Item{Event: Event{Kind: "move", Subject: 1}, To: ToViewers})               // только mc
	h.Emit(Item{Event: Event{Kind: "lonely", Subject: 3}, To: ToViewers})             // некому
	h.Emit(Item{Event: Event{Kind: "lost"}, To: ToOne, Target: 42})                   // нет адресата
	h.Emit(Item{Event: Event{Kind: "ghost", Subject: 42}, To: ToViewers, Self: true}) // нет автора
	h.Emit(Item{Event: Event{Kind: "bad"}, To: 0})
	h.Emit(Item{Event: Event{Kind: "bad"}, To: 99})
	got := render(h.Flush(1))
	want := `2=mc:move:1:; 3=[{"t":"tick","n":1,"msgs":0}]`
	if got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
	if mc.appends != 1 || web.appends != 0 {
		t.Errorf("Append: mc=%d web=%d, want 1 и 0: протокол без получателей не кодирует", mc.appends, web.appends)
	}
}

func TestHiddenSkippedEventNotCounted(t *testing.T) {
	h := NewHub()
	mc := &binProto{name: "mc", skip: map[string]bool{"rotate": true}}
	web := &jsonProto{}
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 2, mc)
	mustAdd(t, h, 3, web)
	h.Emit(Item{Event: Event{Kind: "rotate", Subject: 3}, To: ToAll})
	h.Emit(Item{Event: Event{Kind: "rotate", Subject: 3}, To: ToOne, Target: 1})
	got := render(h.Flush(9))
	want := `3=[{"t":"rotate","id":3,"text":""},{"t":"tick","n":9,"msgs":1}]`
	if got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
	for _, st := range mc.stats {
		if st != (Stats{Tick: 9}) {
			t.Errorf("End получил %+v, want {Tick:9 Events:0}: пустой результат Append — не событие", st)
		}
	}
	if len(mc.stats) != 2 {
		t.Errorf("End для mc вызван %d раз, want 2", len(mc.stats))
	}
}

func TestHiddenStatsPerPlayer(t *testing.T) {
	h := NewHub()
	mc := &binProto{name: "mc"}
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 2, mc)
	mustSee(t, h, 1, 2)
	h.Emit(Item{Event: Event{Kind: "a"}, To: ToAll})
	h.Emit(Item{Event: Event{Kind: "b", Subject: 1}, To: ToViewers})
	h.Emit(Item{Event: Event{Kind: "c"}, To: ToOne, Target: 2})
	h.Flush(4)
	got := slices.Clone(mc.stats)
	slices.SortFunc(got, func(a, b Stats) int { return a.Events - b.Events })
	want := []Stats{{Tick: 4, Events: 1}, {Tick: 4, Events: 3}}
	if !slices.Equal(got, want) {
		t.Errorf("Stats (по возрастанию Events) = %+v, want %+v", got, want)
	}
}

func TestHiddenOrderAndInterleaving(t *testing.T) {
	h := NewHub()
	mc := &binProto{name: "mc"}
	for _, id := range []PlayerID{30, 10, 20} {
		mustAdd(t, h, id, mc)
	}
	mustSee(t, h, 10, 20)
	h.Emit(Item{Event: Event{Kind: "1", Subject: 30}, To: ToOne, Target: 20})
	h.Emit(Item{Event: Event{Kind: "2", Subject: 10}, To: ToViewers, Self: true})
	h.Emit(Item{Event: Event{Kind: "3"}, To: ToAll, Exclude: 10})
	h.Emit(Item{Event: Event{Kind: "4", Subject: 20}, To: ToOne, Target: 20})
	got := render(h.Flush(1))
	want := "10=mc:2:10:; 20=mc:1:30:;mc:2:10:;mc:3:0:;mc:4:20:; 30=mc:3:0:;"
	if got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
}

func TestHiddenDistinctInstances(t *testing.T) {
	// Два экземпляра одного типа — два протокола: у каждого свой кеш.
	h := NewHub()
	a, b := &binProto{name: "a"}, &binProto{name: "b"}
	mustAdd(t, h, 1, a)
	mustAdd(t, h, 2, b)
	mustAdd(t, h, 3, a)
	h.Emit(Item{Event: Event{Kind: "x"}, To: ToAll})
	if got, want := render(h.Flush(1)), "1=a:x:0:; 2=b:x:0:; 3=a:x:0:;"; got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
	if a.appends != 1 || b.appends != 1 {
		t.Errorf("Append: a=%d b=%d, want 1 и 1", a.appends, b.appends)
	}
}

func TestHiddenCacheResetBetweenEvents(t *testing.T) {
	h := NewHub()
	mc := &binProto{name: "mc"}
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 2, mc)
	mustSee(t, h, 1, 2)
	h.Emit(Item{Event: Event{Kind: "chat", Subject: 1, Text: "one"}, To: ToAll})
	h.Emit(Item{Event: Event{Kind: "chat", Subject: 1, Text: "two"}, To: ToAll})
	h.Emit(Item{Event: Event{Kind: "chat", Subject: 2, Text: "three"}, To: ToViewers, Self: true})
	got := render(h.Flush(1))
	want := "1=mc:chat:1:one;mc:chat:1:two;mc:chat:2:three; 2=mc:chat:1:one;mc:chat:1:two;mc:chat:2:three;"
	if got != want {
		t.Errorf("Flush:\n got %s\nwant %s", got, want)
	}
	if mc.appends != 3 {
		t.Errorf("Append вызван %d раз, want 3", mc.appends)
	}
}

func TestHiddenDataIndependent(t *testing.T) {
	h := NewHub()
	mc := &binProto{name: "mc"}
	mustAdd(t, h, 1, mc)
	mustAdd(t, h, 2, mc)
	h.Emit(Item{Event: Event{Kind: "a", Text: "first"}, To: ToAll})
	first := h.Flush(1)
	if len(first) != 2 {
		t.Fatalf("Flush вернул %d пачек, want 2", len(first))
	}
	keep := bytes.Clone(first[1].Data)
	for i := range first[0].Data {
		first[0].Data[i] = '#'
	}
	if !bytes.Equal(first[1].Data, keep) {
		t.Fatalf("пачки игроков делят память: %q", first[1].Data)
	}
	h.Emit(Item{Event: Event{Kind: "b", Text: "second-and-longer"}, To: ToAll})
	h.Emit(Item{Event: Event{Kind: "c", Text: "x"}, To: ToOne, Target: 2})
	second := h.Flush(2)
	if !bytes.Equal(first[1].Data, keep) {
		t.Errorf("следующий Flush изменил прошлую пачку: %q, было %q", first[1].Data, keep)
	}
	if got, want := render(second), "1=mc:b:0:second-and-longer; 2=mc:b:0:second-and-longer;mc:c:0:x;"; got != want {
		t.Errorf("второй Flush:\n got %s\nwant %s", got, want)
	}
}

// eventCheck проверяет, что Append получает событие с теми же полями,
// что и в Item, и что Begin получает пустой dst.
type eventCheck struct {
	t    *testing.T
	want Event
}

func (p *eventCheck) Begin(dst []byte) []byte {
	if len(dst) != 0 {
		p.t.Errorf("Begin получил dst длины %d, want 0", len(dst))
	}
	return dst
}

func (p *eventCheck) Append(dst []byte, ev *Event) []byte {
	if ev == nil || *ev != p.want {
		p.t.Errorf("Append получил %+v, want %+v", ev, p.want)
	}
	return append(dst, 'e')
}

func (p *eventCheck) End(dst []byte, _ Stats) []byte { return dst }

func TestHiddenEventFields(t *testing.T) {
	ev := Event{Kind: "chat", Subject: 2, Text: "привет"}
	h := NewHub()
	p := &eventCheck{t: t, want: ev}
	mustAdd(t, h, 1, p)
	mustAdd(t, h, 2, p)
	mustSee(t, h, 1, 2)
	h.Emit(Item{Event: ev, To: ToViewers, Self: true})
	h.Emit(Item{Event: ev, To: ToOne, Target: 1})
	h.Emit(Item{Event: ev, To: ToAll})
	if got, want := render(h.Flush(1)), "1=eee 2=ee"; got != want {
		t.Errorf("Flush = %s, want %s", got, want)
	}
}

func TestHiddenNoPlayers(t *testing.T) {
	h := NewHub()
	h.Emit(Item{Event: Event{Kind: "a"}, To: ToAll})
	if got := h.Flush(1); len(got) != 0 {
		t.Errorf("Flush без игроков = %v, want пусто", got)
	}
}

// TestHiddenMatchesNaive — случайные тики против наивной рассылки
// (кодирование на каждого получателя): байты те же, а Append вызывается
// ровно один раз на каждую пару «событие × протокол с получателями».
func TestHiddenMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewPCG(13, 15))
	protos := []*binProto{{name: "m"}, {name: "w"}, {name: "x", skip: map[string]bool{"k1": true}}}
	h := NewHub()
	type ref struct {
		proto   *binProto
		visible map[PlayerID]bool
	}
	players := map[PlayerID]*ref{}
	for tick := uint64(1); tick <= 300; tick++ {
		// Изменения состава и видимости.
		for range rng.IntN(4) {
			id := PlayerID(rng.IntN(12) + 1)
			switch rng.IntN(3) {
			case 0:
				pr := protos[rng.IntN(len(protos))]
				err := h.Add(id, pr)
				_, ok := players[id]
				if ok != errors.Is(err, ErrDuplicate) || !ok && err != nil {
					t.Fatalf("тик %d: Add(%d) = %v при наличии=%v", tick, id, err, ok)
				}
				if !ok {
					players[id] = &ref{proto: pr, visible: map[PlayerID]bool{}}
				}
			case 1:
				_, ok := players[id]
				if h.Remove(id) != ok {
					t.Fatalf("тик %d: Remove(%d) != %v", tick, id, ok)
				}
				if ok {
					for q := range players[id].visible {
						delete(players[q].visible, id)
					}
					delete(players, id)
				}
			case 2:
				b := PlayerID(rng.IntN(12) + 1)
				vis := rng.IntN(3) > 0
				err := h.SetVisible(id, b, vis)
				pa, pb := players[id], players[b]
				if id == b || pa == nil || pb == nil {
					if err == nil {
						t.Fatalf("тик %d: SetVisible(%d, %d) без ошибки", tick, id, b)
					}
					continue
				}
				if err != nil {
					t.Fatalf("тик %d: SetVisible(%d, %d) = %v", tick, id, b, err)
				}
				if vis {
					pa.visible[b], pb.visible[id] = true, true
				} else {
					delete(pa.visible, b)
					delete(pb.visible, id)
				}
			}
		}
		// События тика.
		var items []Item
		for range rng.IntN(6) {
			it := Item{
				Event:   Event{Kind: fmt.Sprintf("k%d", rng.IntN(3)), Subject: PlayerID(rng.IntN(13)), Text: fmt.Sprint(tick)},
				To:      Audience(rng.IntN(4)),
				Target:  PlayerID(rng.IntN(13)),
				Exclude: PlayerID(rng.IntN(13)),
				Self:    rng.IntN(2) == 0,
			}
			items = append(items, it)
			h.Emit(it)
		}
		// Наивная рассылка.
		want := map[PlayerID][]byte{}
		wantAppends := map[*binProto]int{}
		for id := range players {
			want[id] = []byte{}
		}
		for _, it := range items {
			var to []PlayerID
			switch it.To {
			case ToOne:
				if players[it.Target] != nil {
					to = append(to, it.Target)
				}
			case ToAll:
				for id := range players {
					if id != it.Exclude {
						to = append(to, id)
					}
				}
			case ToViewers:
				if src := players[it.Event.Subject]; src != nil {
					if it.Self {
						to = append(to, it.Event.Subject)
					}
					for id := range src.visible {
						to = append(to, id)
					}
				}
			}
			used := map[*binProto]bool{}
			for _, id := range to {
				pr := players[id].proto
				used[pr] = true
				if !pr.skip[it.Event.Kind] {
					want[id] = fmt.Appendf(want[id], "%s:%s:%d:%s;", pr.name, it.Event.Kind, it.Event.Subject, it.Event.Text)
				}
			}
			for pr := range used {
				wantAppends[pr]++
			}
		}
		for _, pr := range protos {
			pr.appends = 0
		}
		got := h.Flush(tick)
		var wantBatches []Batch
		for id, b := range want {
			if len(b) > 0 {
				wantBatches = append(wantBatches, Batch{Player: id, Data: b})
			}
		}
		slices.SortFunc(wantBatches, func(a, b Batch) int { return int(a.Player) - int(b.Player) })
		if render(got) != render(wantBatches) {
			t.Fatalf("тик %d:\n got %s\nwant %s", tick, render(got), render(wantBatches))
		}
		for _, pr := range protos {
			if pr.appends != wantAppends[pr] {
				t.Fatalf("тик %d: протокол %s: Append вызван %d раз, want %d", tick, pr.name, pr.appends, wantAppends[pr])
			}
		}
	}
}
