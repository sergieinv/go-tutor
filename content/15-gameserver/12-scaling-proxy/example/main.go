// Мини-прокси в духе Velocity/BungeeCord на loopback. Клиент
// подключается к прокси, прокси выбирает лобби с наименьшей
// нагрузкой и пересылает байты. Команда `/server survival`
// переключает игрока на другой бэкенд без переподключения клиента.
// Бэкенды доверяют только прокси: имя игрока приходит с HMAC-подписью
// общего секрета, а прямое подключение с поддельным именем отвергается.
package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// Учебный протокол: VarInt длины, байт id, данные (урок 01).
const (
	pHello   = iota // клиент → прокси: имя
	pForward        // прокси → бэкенд: имя + HMAC
	pChat           // сервер → клиент: строка
	pSay            // клиент → сервер: строка (и команды прокси)
	pKick           // сервер → клиент: причина отключения
)

func frame(id byte, data []byte) []byte {
	buf := binary.AppendUvarint(nil, uint64(1+len(data)))
	return append(append(buf, id), data...)
}

func readFrame(r *bufio.Reader) (byte, []byte, error) {
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return 0, nil, err
	}
	if n == 0 || n > 4096 {
		return 0, nil, errors.New("плохая длина кадра")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return buf[0], buf[1:], nil
}

// Подпись данных игрока общим секретом прокси и бэкендов — та же
// идея, что modern forwarding в Velocity (HMAC-SHA256).
func sign(secret []byte, name string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(name))
	return m.Sum(nil)
}

// Бэкенд — отдельный «мир». В проде он слушает внутреннюю сеть,
// закрытую файрволом; здесь — loopback.
func backend(world string, secret []byte) net.Listener {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveBackend(world, secret, conn)
		}
	}()
	return ln
}

func serveBackend(world string, secret []byte, conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	id, data, err := readFrame(r)
	if err != nil || id != pForward || len(data) < sha256.Size {
		return
	}
	name, sig := string(data[sha256.Size:]), data[:sha256.Size]
	// Сравнение подписи — только hmac.Equal (постоянное время).
	if !hmac.Equal(sig, sign(secret, name)) {
		conn.Write(frame(pKick, []byte("подключайтесь через прокси")))
		return
	}
	conn.Write(frame(pChat, []byte(world+": добро пожаловать, "+name)))
	for {
		id, data, err := readFrame(r)
		if err != nil {
			return
		}
		if id == pSay {
			conn.Write(frame(pChat, []byte(world+": "+name+" пишет «"+string(data)+"»")))
		}
	}
}

// Прокси знает адреса бэкендов и считает игроков на каждом.
type proxy struct {
	secret  []byte
	addrs   map[string]string
	lobbies []string
	mu      sync.Mutex
	online  map[string]int
}

// Правило выбора: лобби с наименьшим числом игроков, при равенстве —
// первое в списке.
func (p *proxy) pickLobby() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	best := p.lobbies[0]
	for _, l := range p.lobbies[1:] {
		if p.online[l] < p.online[best] {
			best = l
		}
	}
	p.online[best]++
	return best
}

func (p *proxy) leave(server string) {
	p.mu.Lock()
	p.online[server]--
	p.mu.Unlock()
}

// link — соединение с текущим бэкендом и горутина, которая копирует
// его байты клиенту как есть: прокси не разбирает поток сервера.
type link struct {
	conn net.Conn
	done chan struct{}
}

func (p *proxy) connect(server, name string, client net.Conn) (*link, error) {
	conn, err := net.DialTimeout("tcp", p.addrs[server], 2*time.Second)
	if err != nil {
		return nil, err
	}
	payload := append(sign(p.secret, name), name...)
	if _, err := conn.Write(frame(pForward, payload)); err != nil {
		conn.Close()
		return nil, err
	}
	l := &link{conn: conn, done: make(chan struct{})}
	go func() {
		io.Copy(client, conn) // на Linux — splice, без копирования в user space
		close(l.done)
	}()
	return l, nil
}

// close рвёт соединение с бэкендом; io.Copy получает ошибку и выходит.
func (l *link) close() {
	l.conn.Close()
	<-l.done
}

func (p *proxy) serve(client net.Conn) {
	defer client.Close()
	// Для примера — общий дедлайн; в проде его продлевают на каждом
	// чтении и ставят дедлайн записи (урок 05).
	client.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(client)
	id, data, err := readFrame(r)
	if err != nil || id != pHello {
		return
	}
	name := string(data)
	server := p.pickLobby()
	l, err := p.connect(server, name, client)
	if err != nil {
		p.leave(server)
		return
	}
	defer func() {
		if l != nil {
			l.close()
		}
		p.leave(server)
	}()
	for {
		id, data, err := readFrame(r)
		if err != nil {
			return
		}
		// Команды прокси перехватываются, остальное идёт на бэкенд.
		if target, ok := strings.CutPrefix(string(data), "/server "); id == pSay && ok {
			if _, known := p.addrs[target]; !known || target == server {
				client.Write(frame(pChat, []byte("прокси: нельзя перейти на «"+target+"»")))
				continue
			}
			l.close()
			p.leave(server)
			p.mu.Lock()
			p.online[target]++
			p.mu.Unlock()
			server = target
			if l, err = p.connect(server, name, client); err != nil {
				return
			}
			continue
		}
		l.conn.Write(frame(id, data))
	}
}

// Клиент: синхронно шлёт команду и печатает ответ.
type client struct {
	conn net.Conn
	r    *bufio.Reader
}

func dial(addr string) *client {
	conn, _ := net.Dial("tcp", addr)
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	return &client{conn: conn, r: bufio.NewReader(conn)}
}

func (c *client) send(id byte, s string) { c.conn.Write(frame(id, []byte(s))) }

func (c *client) print(who string) {
	id, data, err := readFrame(c.r)
	switch {
	case err != nil:
		fmt.Println(who, "← соединение закрыто")
	case id == pKick:
		fmt.Println(who, "← отключён:", string(data))
	default:
		fmt.Println(who, "←", string(data))
	}
}

func main() {
	secret := []byte("общий секрет прокси и бэкендов") // в проде — из файла/секрета
	p := &proxy{secret: secret, addrs: map[string]string{}, online: map[string]int{}}
	for _, world := range []string{"lobby-1", "lobby-2", "survival"} {
		ln := backend(world, secret)
		defer ln.Close()
		p.addrs[world] = ln.Addr().String()
	}
	p.lobbies = []string{"lobby-1", "lobby-2"}

	pln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer pln.Close()
	var wg sync.WaitGroup
	go func() {
		for {
			conn, err := pln.Accept()
			if err != nil {
				return
			}
			wg.Go(func() { p.serve(conn) })
		}
	}()

	steve := dial(pln.Addr().String())
	steve.send(pHello, "Steve")
	steve.print("Steve")
	alex := dial(pln.Addr().String())
	alex.send(pHello, "Alex")
	alex.print("Alex ")

	steve.send(pSay, "привет")
	steve.print("Steve")
	steve.send(pSay, "/server survival") // то же TCP-соединение клиента
	steve.print("Steve")
	steve.send(pSay, "я в выживании")
	steve.print("Steve")
	steve.send(pSay, "/server nether")
	steve.print("Steve")

	// Бэкенд открыт наружу? Тогда любой назовётся кем угодно —
	// в offline-режиме проверки имени нет. Подпись это закрывает.
	evil := dial(p.addrs["survival"])
	evil.conn.Write(frame(pForward, append(make([]byte, sha256.Size), "Notch"...)))
	evil.print("напрямую")

	steve.conn.Close()
	alex.conn.Close()
	evil.conn.Close()
	pln.Close()
	wg.Wait()
	fmt.Println("игроков на бэкендах после выхода:", p.online["lobby-1"]+p.online["lobby-2"]+p.online["survival"])
}
