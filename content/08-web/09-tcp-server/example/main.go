// TCP-сервер без фреймворков: `net.Listen`, цикл `Accept`
// и горутина на каждое соединение. Протокол — строки:
// команда на строку, ответ на строку. Сервер и клиент
// живут в одной программе.

package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const idleTimeout = 200 * time.Millisecond

// Одно соединение — один обработчик. `defer conn.Close()`
// закрывает его при любом выходе. Перед каждым чтением
// продлеваем дедлайн: молчащий клиент не держит горутину.
func handle(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 1024), 1024) // лимит длины строки
	for {
		conn.SetReadDeadline(time.Now().Add(idleTimeout))
		if !sc.Scan() {
			return // EOF, таймаут или слишком длинная строка
		}
		cmd, arg, _ := strings.Cut(sc.Text(), " ")
		switch cmd {
		case "PING":
			fmt.Fprintln(conn, "PONG")
		case "UPPER":
			fmt.Fprintln(conn, strings.ToUpper(arg))
		case "QUIT":
			fmt.Fprintln(conn, "BYE")
			return
		default:
			fmt.Fprintln(conn, "ERR unknown command")
		}
	}
}

// Цикл `Accept` с ограничением числа соединений
// (семафор на канале) и учётом обработчиков.
func serve(ln net.Listener, wg *sync.WaitGroup) {
	sem := make(chan struct{}, 100)
	for {
		conn, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return // listener закрыт — остановка
		}
		if err != nil {
			continue // временная ошибка: например, кончились дескрипторы
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			handle(conn)
		})
	}
}

func main() {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	var wg sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() { serve(ln, &wg); close(acceptDone) }()

	// Клиент: пишем команду, читаем строку ответа.
	// TCP — поток байт: границы сообщений задаёт `\n`.
	conn, _ := net.Dial("tcp", ln.Addr().String())
	r := bufio.NewReader(conn)
	for _, cmd := range []string{"PING", "UPPER привет, tcp", "HELLO", "QUIT"} {
		fmt.Fprintln(conn, cmd)
		reply, _ := r.ReadString('\n')
		fmt.Printf("%s → %s", cmd, reply)
	}
	_, err := r.ReadString('\n')
	fmt.Println("после QUIT:", err)
	conn.Close()

	// Молчаливый клиент: сервер закроет соединение по дедлайну.
	idle, _ := net.Dial("tcp", ln.Addr().String())
	_, err = bufio.NewReader(idle).ReadString('\n')
	fmt.Println("молчаливый клиент:", err)
	idle.Close()

	// Остановка: закрыть listener — `Accept` вернёт
	// `net.ErrClosed`; затем дождаться обработчиков.
	ln.Close()
	<-acceptDone
	wg.Wait()
	fmt.Println("сервер остановлен")
}
