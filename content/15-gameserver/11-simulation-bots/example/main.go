// Один и тот же код мира работает в двух режимах. Сначала — сервер
// на loopback и восемь ботов-горутин: каждый входит, делает десять
// шагов, пишет в чат и выходит. Итог детерминирован, хотя пакеты
// приходят в разном порядке. Потом — тот же мир без сети:
// детерминированная симуляция с зерном, журналом ввода и повтором.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"maps"
	"math/rand/v2"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
)

// Учебный протокол: VarInt длины, байт id, данные (урок 01).
const (
	pLogin   = iota // клиент → сервер: имя
	pStart          // все вошли — можно ходить
	pMove           // шаг dx, dz ∈ {-1, 0, 1}
	pPos            // подтверждение: новая позиция
	pChat           // сообщение в чат
	pChatAll        // рассылка чата всем
	pDone           // бот закончил сценарий
	pEnd            // все закончили — можно уходить
	pLeave          // внутренняя команда: соединение закрыто
)

func frame(id byte, data ...byte) []byte {
	buf := binary.AppendUvarint(nil, uint64(1+len(data)))
	return append(append(buf, id), data...)
}

func readFrame(r *bufio.Reader) (byte, []byte, error) {
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return 0, nil, err
	}
	if n == 0 || n > 1024 { // лимит — до выделения памяти
		return 0, nil, errors.New("плохая длина кадра")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return buf[0], buf[1:], nil
}

// Команда в мир. В тике команды применяются не в порядке прихода,
// а в каноническом: по имени игрока, а у одного игрока — в порядке
// его соединения. Так результат тика не зависит от планировщика.
type cmd struct {
	name   string
	kind   byte
	dx, dz int8
	text   string
	out    chan []byte // очередь исходящих соединения, от которого команда
}

type player struct {
	x, z, sx, sz int32
	moves        int32
	out          chan []byte
}

// Мир принадлежит одной горутине. В симуляции у игроков нет
// соединений (`out == nil`), и отправка — просто пропуск.
type world struct {
	tick       uint64
	expected   int
	players    map[string]*player
	pending    []cmd
	joined     int
	done       int
	bytesOut   int
	violations int
}

func newWorld(expected int) *world {
	return &world{expected: expected, players: map[string]*player{}}
}

func (w *world) send(p *player, pkt []byte) {
	select {
	case p.out <- pkt:
		w.bytesOut += len(pkt)
	default: // nil-канал в симуляции; в сервере — медленный клиент (урок 05)
	}
}

func (w *world) broadcast(pkt []byte) {
	for _, name := range slices.Sorted(maps.Keys(w.players)) {
		w.send(w.players[name], pkt)
	}
}

func (w *world) apply(c cmd) {
	p := w.players[c.name]
	switch {
	case p != nil && p.out != c.out:
		// Имя занято другим соединением. Не верим клиенту на слово:
		// второй вход отвергаем (закрыть его очередь — писатель
		// закроет соединение), а прочие команды чужого соединения,
		// включая его pLeave, игнорируем — они не трогают игрока.
		if c.kind == pLogin && c.out != nil {
			close(c.out)
		}
	case c.kind == pLogin && p == nil:
		h := fnv.New32a()
		h.Write([]byte(c.name))
		x, z := int32(h.Sum32()%16), int32(h.Sum32()/16%16)
		w.players[c.name] = &player{x: x, z: z, sx: x, sz: z, out: c.out}
		if w.joined++; w.joined == w.expected {
			w.broadcast(frame(pStart))
		}
	case p == nil || c.kind == pLogin:
		// команда от неизвестного или ушедшего игрока, повторный вход — игнор
	case c.kind == pMove:
		if c.dx >= -1 && c.dx <= 1 && c.dz >= -1 && c.dz <= 1 {
			p.x, p.z, p.moves = p.x+int32(c.dx), p.z+int32(c.dz), p.moves+1
		}
		pos := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, uint32(p.x)), uint32(p.z))
		w.send(p, frame(pPos, pos...))
	case c.kind == pChat:
		w.broadcast(frame(pChatAll, []byte(c.name+": "+c.text)...))
	case c.kind == pDone:
		if w.done++; w.done == w.expected {
			w.broadcast(frame(pEnd))
		}
	case c.kind == pLeave:
		if p.out != nil {
			close(p.out)
		}
		delete(w.players, c.name)
	}
}

// Тик: собрать ввод, применить в каноническом порядке, проверить
// инварианты. Нарушение инварианта в тесте — сразу падение.
func (w *world) step() {
	w.tick++
	slices.SortStableFunc(w.pending, func(a, b cmd) int {
		return strings.Compare(a.name, b.name)
	})
	for _, c := range w.pending {
		w.apply(c)
	}
	w.pending = w.pending[:0]
	for _, p := range w.players {
		dx, dz := abs(p.x-p.sx), abs(p.z-p.sz)
		if dx > p.moves || dz > p.moves || len(w.players) > w.expected {
			w.violations++
		}
	}
}

// Хеш состояния: тик и игроки в порядке имён. Обход map напрямую
// дал бы разный хеш на каждом запуске.
func (w *world) hash() uint64 {
	h := fnv.New64a()
	h.Write(binary.BigEndian.AppendUint64(nil, w.tick))
	for _, name := range slices.Sorted(maps.Keys(w.players)) {
		p := w.players[name]
		h.Write([]byte(name))
		h.Write(binary.BigEndian.AppendUint64(nil, uint64(uint32(p.x))<<32|uint64(uint32(p.z))))
	}
	return h.Sum64()
}

// Сервер: на соединение — горутина чтения и горутина записи;
// мир тикает по каналу `ticks` (в проде — `time.Ticker`, в тестах —
// ручной канал). Мир завершается, когда все вошли и все вышли.
func serve(ln net.Listener, w *world, ticks <-chan time.Time) {
	in := make(chan cmd, 256)
	var conns sync.WaitGroup
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			go handle(conn, in, &conns)
		}
	}()
	for w.joined < w.expected || len(w.players) > 0 {
		select {
		case c := <-in:
			w.pending = append(w.pending, c)
		case <-ticks:
			w.step()
		}
	}
	ln.Close()
	conns.Wait()
}

func handle(conn net.Conn, in chan<- cmd, conns *sync.WaitGroup) {
	defer conns.Done()
	r := bufio.NewReader(conn)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	id, data, err := readFrame(r)
	if err != nil || id != pLogin {
		conn.Close()
		return
	}
	name, out := string(data), make(chan []byte, 64)
	var writer sync.WaitGroup
	writer.Go(func() {
		for pkt := range out { // мир закрывает out, когда игрок ушёл
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			conn.Write(pkt)
		}
		conn.Close() // разбудить читателя, если вход отвергнут
	})
	in <- cmd{name: name, kind: pLogin, out: out}
	for {
		id, data, err := readFrame(r)
		if err != nil {
			break
		}
		if id != pMove && id != pChat && id != pDone {
			continue // внутренние и неизвестные команды от клиента не принимаем
		}
		c := cmd{name: name, kind: id, out: out}
		if id == pMove && len(data) == 2 {
			c.dx, c.dz = int8(data[0]), int8(data[1])
		}
		c.text = string(data)
		in <- c
	}
	in <- cmd{name: name, kind: pLeave, out: out}
	writer.Wait()
	conn.Close()
}

// Бот — обычный синхронный клиент: послал, дождался ответа.
// Ходы — из генератора с зерном бота, так что сценарий повторим.
type botResult struct {
	name  string
	x, z  int32
	chats int
}

func bot(addr, name string, seed uint64) (res botResult, err error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return res, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	res.name = name
	// until читает пакеты, пока не придёт нужный, по пути считая чат.
	until := func(want byte) ([]byte, error) {
		for {
			id, data, err := readFrame(r)
			if err != nil || id == want {
				return data, err
			}
			if id == pChatAll {
				res.chats++
			}
		}
	}
	conn.Write(frame(pLogin, []byte(name)...))
	if _, err := until(pStart); err != nil {
		return res, err
	}
	rng := rand.New(rand.NewPCG(seed, 0))
	for i := range 10 {
		dx, dz := int8(rng.Uint64()%3)-1, int8(rng.Uint64()%3)-1
		conn.Write(frame(pMove, byte(dx), byte(dz)))
		pos, err := until(pPos)
		if err != nil || len(pos) != 8 {
			return res, fmt.Errorf("нет позиции: %v", err)
		}
		res.x, res.z = int32(binary.BigEndian.Uint32(pos)), int32(binary.BigEndian.Uint32(pos[4:]))
		if i == 4 {
			conn.Write(frame(pChat, []byte("привет")...))
		}
	}
	conn.Write(frame(pDone))
	_, err = until(pEnd)
	return res, err
}

func main() {
	const bots = 8
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	w := newWorld(bots)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	served := make(chan struct{})
	go func() { serve(ln, w, ticker.C); close(served) }()

	results := make([]botResult, bots)
	var wg sync.WaitGroup
	for i := range bots {
		wg.Go(func() {
			r, err := bot(ln.Addr().String(), fmt.Sprintf("bot-%d", i), uint64(i))
			if err != nil {
				r.name = "ошибка: " + err.Error()
			}
			results[i] = r
		})
	}
	wg.Wait()
	<-served
	fmt.Println("нагрузочный прогон:", bots, "ботов")
	for _, r := range results {
		fmt.Printf("  %s: позиция (%d, %d), получено сообщений чата %d\n", r.name, r.x, r.z, r.chats)
	}
	fmt.Println("  исходящих байт:", w.bytesOut, "| нарушений инвариантов:", w.violations, "| онлайн:", len(w.players))

	// Тот же мир без сети: тики вручную, ввод — от ботов с зерном,
	// каждая применённая команда пишется в журнал вместе с тиком.
	type rec struct {
		tick uint64
		c    cmd
	}
	simulate := func(seed uint64) (uint64, []rec) {
		w := newWorld(bots)
		rngs := make([]*rand.Rand, bots)
		var log []rec
		for i := range rngs {
			rngs[i] = rand.New(rand.NewPCG(seed, uint64(i)))
			w.pending = append(w.pending, cmd{name: fmt.Sprintf("bot-%d", i), kind: pLogin})
		}
		for _, c := range w.pending {
			log = append(log, rec{1, c})
		}
		w.step() // тик 1 — вход
		for range 100 {
			for i, rng := range rngs {
				c := cmd{name: fmt.Sprintf("bot-%d", i), kind: pMove, dx: int8(rng.Uint64()%3) - 1, dz: int8(rng.Uint64()%3) - 1}
				w.pending = append(w.pending, c)
			}
			for _, c := range w.pending {
				log = append(log, rec{w.tick + 1, c})
			}
			w.step()
		}
		return w.hash(), log
	}
	replay := func(log []rec, reverse bool) uint64 {
		w := newWorld(bots)
		for i := 0; i < len(log); {
			j := i
			for j < len(log) && log[j].tick == log[i].tick {
				w.pending = append(w.pending, log[j].c)
				j++
			}
			// Порядок прихода от разных игроков не важен: тик сортирует.
			// Порядок команд одного игрока задаёт его TCP-соединение.
			if reverse {
				slices.Reverse(w.pending)
			}
			w.step()
			i = j
		}
		return w.hash()
	}
	h1, log := simulate(42)
	h2, _ := simulate(42)
	h3, _ := simulate(43)
	fmt.Println("симуляция: 101 тик,", len(log), "команд в журнале")
	fmt.Println("  тот же сид — тот же хеш:", h1 == h2)
	fmt.Println("  повтор из журнала совпал:", replay(log, false) == h1)
	fmt.Println("  ввод внутри тика в обратном порядке:", replay(log, true) == h1)
	fmt.Println("  другой сид — другой хеш:", h3 != h1)
}

func abs(v int32) int32 { return max(v, -v) }
