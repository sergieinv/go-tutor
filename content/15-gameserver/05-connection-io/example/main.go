// Две горутины на соединение: читатель разбирает кадры и шлёт
// команды в мир, писатель вычерпывает исходящую очередь в сокет.
// Мир пишет только в очередь и никогда не ждёт: медленный клиент
// переполняет свою очередь и отключается, остальные этого не
// замечают. Пример поднимает сервер на loopback, подключает
// быстрого, медленного и молчащего ботов и печатает только
// детерминированное: кто отключён и почему, сколько горутин осталось.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"time"
)

var (
	errSlow     = errors.New("исходящая очередь переполнена")
	errShutdown = errors.New("сервер остановлен")
)

const maxFrame = 1 << 16 // лимит длины кадра (урок 01)

// Кадр: длина (uvarint) и данные. Своё кадрирование с VarInt —
// урок 01, здесь — `encoding/binary` для краткости.
func writeFrame(w io.Writer, p []byte) error {
	buf := binary.AppendUvarint(make([]byte, 0, len(p)+3), uint64(len(p)))
	_, err := w.Write(append(buf, p...))
	return err
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, err
	}
	if n > maxFrame {
		return nil, fmt.Errorf("кадр %d байт больше лимита", n)
	}
	p := make([]byte, n)
	_, err = io.ReadFull(r, p)
	return p, err
}

type Config struct {
	Queue        int           // длина исходящей очереди
	ReadTimeout  time.Duration // клиент молчит дольше — отключаем
	WriteTimeout time.Duration // один кадр пишется дольше — отключаем
}

// Client — соединение со стороны сервера. `out` — ограниченная
// очередь, `done` закрывается ровно один раз при отключении.
type Client struct {
	Name   string
	conn   net.Conn
	out    chan []byte
	done   chan struct{}
	once   sync.Once
	reason error
}

// Close можно звать из любой горутины сколько угодно раз:
// сработает первый, его причина и останется. Закрытие сокета
// будит читателя и писателя, если они висят в Read/Write.
func (c *Client) Close(reason error) {
	c.once.Do(func() {
		c.reason = reason
		close(c.done)
		c.conn.Close()
	})
}

// Send вызывает тик. Он никогда не ждёт: нет места — клиент
// отключается. Канал `out` не закрывают никогда — иначе Send
// из тика паниковал бы на закрытом канале.
func (c *Client) Send(p []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.out <- p:
		return true
	default:
		c.Close(errSlow)
		return false
	}
}

// Писатель: каждый кадр — с дедлайном записи.
func (c *Client) writeLoop(cfg Config) {
	for {
		select {
		case <-c.done:
			return
		case p := <-c.out:
			c.conn.SetWriteDeadline(time.Now().Add(cfg.WriteTimeout))
			if err := writeFrame(c.conn, p); err != nil {
				c.Close(fmt.Errorf("запись: %w", err))
				return
			}
		}
	}
}

// Читатель: дедлайн чтения продлевается перед каждым кадром.
// Первый кадр — имя игрока, дальше — команды в мир.
func (c *Client) readLoop(cfg Config, join chan<- *Client) {
	r := bufio.NewReader(c.conn)
	for first := true; ; first = false {
		c.conn.SetReadDeadline(time.Now().Add(cfg.ReadTimeout))
		p, err := readFrame(r)
		if err != nil {
			c.Close(fmt.Errorf("чтение: %w", err))
			return
		}
		if first {
			c.Name = string(p)
			select {
			case join <- c: // мир читает join в каждом тике
			case <-c.done: // отключён раньше (остановка сервера) — не ждём мир
				return
			}
		}
	}
}

type Server struct {
	cfg     Config
	ln      net.Listener
	join    chan *Client
	ticks   chan chan Report
	clients map[*Client]bool // принадлежит горутине мира

	mu       sync.Mutex
	all      []*Client     // все принятые — для остановки
	accepted chan struct{} // закрыт, когда accept вышел
	conns    sync.WaitGroup
}

// Report — что произошло за тик: кому кадр ушёл в очередь,
// кто отключён.
type Report struct {
	Sent []string
	Gone []*Client
}

func NewServer(cfg Config) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, ln: ln, join: make(chan *Client, 16), ticks: make(chan chan Report),
		clients: map[*Client]bool{}, accepted: make(chan struct{})}
	go s.accept()
	go s.world()
	return s, nil
}

func (s *Server) accept() {
	defer close(s.accepted)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return // слушатель закрыт
		}
		conn.(*net.TCPConn).SetWriteBuffer(4096) // маленький буфер — быстрее видно медленного
		c := &Client{conn: conn, out: make(chan []byte, s.cfg.Queue), done: make(chan struct{})}
		s.mu.Lock()
		s.all = append(s.all, c)
		s.mu.Unlock()
		s.conns.Add(2)
		go func() { defer s.conns.Done(); c.readLoop(s.cfg, s.join) }()
		go func() { defer s.conns.Done(); c.writeLoop(s.cfg) }()
	}
}

// Горутина мира: тик = принять новых, разослать состояние.
// Отправка не ждёт никого — `Send` либо кладёт кадр в очередь,
// либо отключает клиента.
func (s *Server) world() {
	update := make([]byte, 2048) // «состояние мира» для рассылки
	for reply := range s.ticks {
	join:
		for {
			select {
			case c := <-s.join:
				s.clients[c] = true
			default:
				break join
			}
		}
		var r Report
		for c := range s.clients {
			if c.Send(update) {
				r.Sent = append(r.Sent, c.Name)
			} else {
				delete(s.clients, c)
				r.Gone = append(r.Gone, c)
			}
		}
		reply <- r
	}
}

// Tick просит мир выполнить тик и ждёт отчёт. В сервере тики
// задаёт игровой цикл (урок 04), здесь — main.
func (s *Server) Tick() Report {
	reply := make(chan Report)
	s.ticks <- reply
	return <-reply
}

// Остановка: не принимать новых, остановить мир, отключить всех
// и дождаться обеих горутин каждого соединения.
func (s *Server) Shutdown() {
	s.ln.Close()
	<-s.accepted
	close(s.ticks)
	s.mu.Lock()
	for _, c := range s.all {
		c.Close(errShutdown)
	}
	s.mu.Unlock()
	s.conns.Wait()
}

// Бот: подключается и представляется.
func dial(s *Server, name string) net.Conn {
	conn, err := net.Dial("tcp", s.ln.Addr().String())
	if err != nil {
		panic(err)
	}
	writeFrame(conn, []byte(name))
	return conn
}

func has(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func main() {
	before := runtime.NumGoroutine()
	s, err := NewServer(Config{Queue: 8, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
	if err != nil {
		panic(err)
	}

	// Быстрый бот читает каждый кадр и сообщает о нём в got.
	fast := dial(s, "fast")
	got := make(chan struct{})
	go func() {
		defer close(got)
		r := bufio.NewReader(fast)
		for {
			if _, err := readFrame(r); err != nil {
				return
			}
			got <- struct{}{}
		}
	}()

	// Тик и учёт кадров быстрого бота. После каждого тика ждём,
	// пока быстрый прочитает свой кадр, — это темп примера, сервер
	// никого не ждёт.
	sentFast, recvFast := 0, 0
	tick := func() Report {
		r := s.Tick()
		if has(r.Sent, "fast") {
			sentFast++
			if _, ok := <-got; ok {
				recvFast++
			}
		}
		return r
	}
	// Читатель передаёт бота в мир асинхронно: тикаем, пока
	// мир не начнёт слать ему кадры.
	for !has(tick().Sent, "fast") {
	}

	// Медленный бот не читает вовсе: буферы сокетов в ядре
	// заполнятся, писатель встанет в Write, очередь переполнится.
	slow := dial(s, "slow")
	slow.(*net.TCPConn).SetReadBuffer(4096)
	for !has(tick().Sent, "slow") {
	}

	// Тики, пока медленного не отключат.
	var slowReason error
	for slowReason == nil {
		for _, c := range tick().Gone {
			<-c.done
			slowReason = c.reason
		}
	}
	fmt.Println("медленный бот отключён:", slowReason)

	// Мир тикает дальше, быстрый бот ничего не заметил.
	for range 5 {
		tick()
	}
	fmt.Println("быстрый бот получил каждый свой кадр:", recvFast == sentFast)

	// Молчащий бот: подключился, представился и молчит. Через
	// ReadTimeout сервер его отключит.
	quiet, err := NewServer(Config{Queue: 8, ReadTimeout: 100 * time.Millisecond, WriteTimeout: time.Second})
	if err != nil {
		panic(err)
	}
	silent := dial(quiet, "silent")
	io.Copy(io.Discard, silent) // вернётся, когда сервер закроет соединение
	var silentReason error
	for silentReason == nil {
		for _, c := range quiet.Tick().Gone {
			<-c.done
			silentReason = c.reason
		}
	}
	fmt.Println("молчащий бот отключён по дедлайну чтения:", isTimeout(silentReason))

	s.Shutdown()
	quiet.Shutdown()
	for _, c := range []net.Conn{fast, slow, silent} {
		c.Close()
	}
	<-got
	fmt.Println("горутины соединений завершились")
	for range 100 { // рантайм отпускает горутины не мгновенно
		if runtime.NumGoroutine() <= before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Println("утечек горутин нет:", runtime.NumGoroutine() <= before)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
