// Состояния соединения игрового сервера: handshake → status (пинг
// списка серверов) или login → play. Сервер слушает 127.0.0.1:0,
// боты подключаются по очереди и печатают, что получили. Вывод
// печатают только боты из main, поэтому он не зависит от планировщика.
package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Состояние — обычный enum. Один и тот же id пакета в разных
// состояниях значит разное: 0x00 — это Handshake, Status Request
// или Login Start, смотря когда пришёл.
type state int

const (
	handshake state = iota
	status
	login
	play
)

func (s state) String() string { return [...]string{"handshake", "status", "login", "play"}[s] }

type conn struct {
	nc    net.Conn
	br    *bufio.Reader
	wmu   sync.Mutex // пишут и своя горутина, и рассылка чата
	state state
	name  string
	ka    int64 // ждём ответа на этот keep-alive; 0 — не ждём
}

type handler func(s *server, c *conn, p *packet) error

// Таблица «состояние × id → обработчик» — весь протокол на одном
// экране. Пакета нет в таблице — ошибка и разрыв: клиент, который
// прислал Chat в handshake, либо сломан, либо атакует.
var handlers = map[state]map[int32]handler{
	handshake: {0x00: onHandshake},
	status:    {0x00: onStatusRequest, 0x01: onPing},
	login:     {0x00: onLoginStart},
	play:      {0x01: onKeepAlive, 0x02: onChat},
}

func onHandshake(s *server, c *conn, p *packet) error {
	p.varint() // версия протокола: учебный сервер принимает любую
	p.str(255) // адрес, по которому клиент подключался
	p.u16()    // порт
	switch next := p.varint(); next {
	case 1:
		c.state = status
	case 2:
		c.state = login
	default:
		return fmt.Errorf("next state %d", next)
	}
	return p.err
}

// Ответ на пинг списка серверов — JSON. Поля и вложенность — как у
// Minecraft; клиент показывает версию, «онлайн/максимум» и описание.
type statusJSON struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Players struct {
		Max    int      `json:"max"`
		Online int      `json:"online"`
		Sample []player `json:"sample,omitempty"`
	} `json:"players"`
	Description struct {
		Text string `json:"text"`
	} `json:"description"`
}

type player struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

func onStatusRequest(s *server, c *conn, p *packet) error {
	var st statusJSON
	st.Version.Name, st.Version.Protocol = "gotutor", 1
	st.Players.Max = 20
	st.Description.Text = "Учебный сервер"
	s.mu.Lock()
	for _, o := range s.players {
		st.Players.Online++
		st.Players.Sample = append(st.Players.Sample, player{o.name, uuidString(offlineUUID(o.name))})
	}
	s.mu.Unlock()
	js, _ := json.Marshal(st)
	return c.send(0x00, appendStr(nil, string(js)))
}

// Ping несёт произвольное число, Pong возвращает его как есть —
// клиент меряет по нему задержку. После Pong соединение закрывается.
func onPing(s *server, c *conn, p *packet) error {
	v := p.i64()
	if p.err != nil {
		return p.err
	}
	c.send(0x01, binary.BigEndian.AppendUint64(nil, uint64(v)))
	return io.EOF
}

// Offline-режим: пароля нет, имя — всё, что известно. UUID игрока —
// версия 3 (MD5) от строки "OfflinePlayer:"+имя, как у Minecraft:
// один и тот же ник на любом offline-сервере получает тот же UUID.
func offlineUUID(name string) [16]byte {
	u := md5.Sum([]byte("OfflinePlayer:" + name))
	u[6] = u[6]&0x0f | 0x30 // версия 3 — старшие 4 бита байта 6
	u[8] = u[8]&0x3f | 0x80 // вариант RFC 4122 — биты 10 в байте 8
	return u
}

func uuidString(u [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

func onLoginStart(s *server, c *conn, p *packet) error {
	name := p.str(16)
	if p.err != nil || name == "" {
		return errors.New("bad name")
	}
	c.name, c.state = name, play
	s.mu.Lock()
	s.players[c] = c
	s.mu.Unlock()
	u := offlineUUID(name)
	if err := c.send(0x02, appendStr(u[:], name)); err != nil {
		return err
	}
	// Первый keep-alive сразу после входа. Настоящий сервер шлёт
	// их регулярно и отключает клиента, не ответившего за 15 секунд;
	// ожидание ответа здесь — дедлайн чтения соединения.
	c.ka = 1
	c.nc.SetReadDeadline(time.Now().Add(s.kaTimeout))
	return c.send(0x01, binary.BigEndian.AppendUint64(nil, uint64(c.ka)))
}

// Ответ на keep-alive должен вернуть то же число. Чужое число —
// клиент не тот, за кого себя выдаёт, или сломан.
func onKeepAlive(s *server, c *conn, p *packet) error {
	if v := p.i64(); p.err != nil || v != c.ka || c.ka == 0 {
		return errors.New("bad keep-alive")
	}
	c.ka = 0
	c.nc.SetReadDeadline(time.Time{}) // следующий keep-alive снова поставит дедлайн
	return nil
}

// Чат — недоверенная строка: лимит, валидный UTF-8 и никаких
// управляющих символов (перевод строки подделал бы чужую строку чата).
func onChat(s *server, c *conn, p *packet) error {
	msg := p.str(256)
	if p.err != nil || msg == "" || !utf8.ValidString(msg) || strings.ContainsFunc(msg, unicode.IsControl) {
		return errors.New("bad chat")
	}
	line := appendStr(nil, "<"+c.name+"> "+msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.players {
		o.send(0x02, line)
	}
	return nil
}

// Цикл соединения: прочитать кадр, найти обработчик по текущему
// состоянию, вызвать. Любая ошибка — Disconnect с причиной (если
// протокол в этом состоянии его знает) и закрытие.
func (s *server) serve(c *conn) {
	defer s.drop(c)
	for {
		id, data, err := readFrame(c.br)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && c.ka != 0 {
				c.send(0x03, appendStr(nil, "keep-alive timeout"))
			}
			return
		}
		h := handlers[c.state][id]
		if h == nil {
			err = fmt.Errorf("unexpected packet 0x%02x in state %v", id, c.state)
		} else {
			err = h(s, c, &packet{b: data})
		}
		if err != nil {
			if c.state == play && err != io.EOF {
				c.send(0x03, appendStr(nil, err.Error()))
			}
			return
		}
	}
}

type server struct {
	ln        net.Listener
	kaTimeout time.Duration
	mu        sync.Mutex
	players   map[*conn]*conn
	conns     sync.WaitGroup
}

func (s *server) drop(c *conn) {
	s.mu.Lock()
	delete(s.players, c)
	s.mu.Unlock()
	c.nc.Close()
}

func (s *server) acceptLoop() {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			return // listener закрыт
		}
		c := &conn{nc: nc, br: bufio.NewReader(nc)}
		s.conns.Go(func() { s.serve(c) })
	}
}

func (c *conn) send(id int32, data []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.nc.SetWriteDeadline(time.Now().Add(time.Second))
	_, err := c.nc.Write(appendFrame(nil, id, data))
	return err
}

// Кадрирование и поля — как в уроке 01, в сокращённом виде.
func appendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for ; u >= 0x80; u >>= 7 {
		b = append(b, byte(u)|0x80)
	}
	return append(b, byte(u))
}

func readVarInt(r io.ByteReader) (int32, error) {
	var u uint32
	for i := range 5 {
		c, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		u |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return int32(u), nil
		}
	}
	return 0, errors.New("varint too long")
}

func appendFrame(b []byte, id int32, data []byte) []byte {
	body := append(appendVarInt(nil, id), data...)
	return append(appendVarInt(b, int32(len(body))), body...)
}

func readFrame(br *bufio.Reader) (int32, []byte, error) {
	n, err := readVarInt(br)
	if err != nil {
		return 0, nil, err
	}
	if n < 1 || n > 4096 {
		return 0, nil, errors.New("bad frame length")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(br, body); err != nil {
		return 0, nil, err
	}
	r := bytes.NewReader(body)
	id, err := readVarInt(r)
	return id, body[len(body)-r.Len():], err
}

func appendStr(b []byte, s string) []byte { return append(appendVarInt(b, int32(len(s))), s...) }

// packet — курсор по полям; первая ошибка запоминается, остальные
// чтения возвращают нули.
type packet struct {
	b   []byte
	err error
}

func (p *packet) take(n int) []byte {
	if p.err != nil || n < 0 || n > len(p.b) {
		if p.err == nil {
			p.err = errors.New("short packet")
		}
		return make([]byte, min(max(n, 0), 16)) // нули, но не больше 16 байт
	}
	v := p.b[:n]
	p.b = p.b[n:]
	return v
}

func (p *packet) varint() int32 {
	r := bytes.NewReader(p.b)
	v, err := readVarInt(r)
	if err != nil {
		p.err = err
	}
	p.b = p.b[len(p.b)-r.Len():]
	return v
}

func (p *packet) str(max int) string {
	n := p.varint()
	if int(n) > max {
		p.err = errors.New("string too long")
	}
	return string(p.take(int(n)))
}

func (p *packet) u16() uint16 { return binary.BigEndian.Uint16(p.take(2)) }
func (p *packet) i64() int64  { return int64(binary.BigEndian.Uint64(p.take(8))) }

// bot — клиент: пишет кадры и читает ответы с коротким дедлайном.
type bot struct {
	nc net.Conn
	br *bufio.Reader
}

func dial(addr string) *bot {
	nc, err := net.Dial("tcp", addr)
	if err != nil {
		panic(err)
	}
	return &bot{nc, bufio.NewReader(nc)}
}

func (b *bot) send(id int32, data []byte) { b.nc.Write(appendFrame(nil, id, data)) }

func (b *bot) recv() (int32, *packet) {
	b.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	id, data, err := readFrame(b.br)
	if err != nil {
		return -1, &packet{err: err}
	}
	return id, &packet{b: data}
}

func (b *bot) handshake(next int32) {
	hs := appendVarInt(nil, 1)
	hs = appendStr(hs, "localhost")
	hs = binary.BigEndian.AppendUint16(hs, 25565)
	b.send(0x00, appendVarInt(hs, next))
}

func (b *bot) login(name string) {
	b.handshake(2)
	b.send(0x00, appendStr(nil, name))
	id, p := b.recv()
	u := p.take(16)
	fmt.Printf("%s: login success 0x%02x, uuid %s, имя %s\n", name, id, uuidString([16]byte(u)), p.str(16))
}

func (b *bot) expectDisconnect(who string) {
	for {
		id, p := b.recv()
		switch {
		case id == 0x03:
			fmt.Printf("%s: disconnect: %s\n", who, p.str(256))
			return
		case id < 0:
			fmt.Printf("%s: соединение закрыто\n", who)
			return
		}
	}
}

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	// Таймаут keep-alive в примере — полсекунды, чтобы Lazy не ждать долго.
	s := &server{ln: ln, kaTimeout: 500 * time.Millisecond, players: map[*conn]*conn{}}
	go s.acceptLoop()
	addr := ln.Addr().String()

	// Steve входит, отвечает на keep-alive и пишет в чат.
	steve := dial(addr)
	steve.login("Steve")
	id, p := steve.recv()
	ka := p.i64()
	fmt.Printf("Steve: keep-alive 0x%02x, число %d — возвращаем\n", id, ka)
	steve.send(0x01, binary.BigEndian.AppendUint64(nil, uint64(ka)))
	steve.send(0x02, appendStr(nil, "привет"))
	id, p = steve.recv()
	fmt.Printf("Steve: чат 0x%02x: %s\n", id, p.str(256))

	// Пинг списка серверов, пока Steve в игре.
	pinger := dial(addr)
	pinger.handshake(1)
	pinger.send(0x00, nil)
	_, p = pinger.recv()
	fmt.Println("пинг: status", p.str(32767))
	pinger.send(0x01, binary.BigEndian.AppendUint64(nil, 20251109))
	_, p = pinger.recv()
	fmt.Println("пинг: pong", p.i64())
	pinger.expectDisconnect("пинг")

	// Lazy входит и не отвечает на keep-alive.
	lazy := dial(addr)
	lazy.login("Lazy")
	lazy.expectDisconnect("Lazy")

	// Пакет не из своего состояния: Status Request (0x00) в play.
	steve.send(0x00, nil)
	steve.expectDisconnect("Steve")

	// Неизвестное next state в handshake.
	odd := dial(addr)
	odd.handshake(7)
	odd.expectDisconnect("next state 7")

	ln.Close()
	s.conns.Wait()
	fmt.Println("сервер остановлен, игроков:", len(s.players))
}
