---
title: TCP-сервер
summary: net.Listen и цикл Accept, горутина на соединение, строковый протокол на bufio, кадрирование через разделитель или префикс длины, дедлайны, лимиты и остановка по net.ErrClosed.
difficulty: 4
minutes: 30
prerequisites:
  - 05-concurrency/11-waitgroup
  - 06-stdlib/12-reading-files
  - 08-web/08-graceful-shutdown
docs:
  - title: "Пакет net"
    url: https://pkg.go.dev/net
  - title: "Пакет encoding/binary"
    url: https://pkg.go.dev/encoding/binary
  - title: "Пакет bufio: Scanner"
    url: https://pkg.go.dev/bufio#Scanner
runner:
  race: true
---

Под `net/http` лежит обычный TCP, и свой сервер на нём пишется в несколько строк: `net.Listen("tcp", addr)` возвращает `net.Listener`, цикл `ln.Accept()` выдаёт `net.Conn` на каждого клиента, а каждое соединение обслуживает своя горутина. Горутины дешёвые, поэтому модель «одна горутина — одно соединение» с блокирующими `Read`/`Write` масштабируется на десятки тысяч клиентов: под капотом рантайм всё равно использует epoll/kqueue и не держит поток ОС на каждое ожидание. `net.Conn` — это `io.Reader` и `io.Writer`, так что работают `bufio`, `fmt.Fprintf`, `io.Copy` и всё остальное из мира потоков.

Главное, что надо помнить о TCP: это **поток байт, а не сообщений**. Два `Write` по 10 байт могут прийти одним `Read` на 20 байт или тремя по 7, 7 и 6. Границы сообщений задаёт протокол — кадрирование. Два стандартных способа: разделитель (строка до `\n` — `bufio.Scanner` или `Reader.ReadString`) и префикс длины (4 байта длины через `encoding/binary`, затем данные — читать их нужно `io.ReadFull`). В обоих случаях нужен лимит размера: иначе клиент пришлёт «строку» в гигабайт или заголовок с длиной 4 ГБ и положит сервер по памяти.

Сервер должен защищаться и от тех, кто молчит: `conn.SetReadDeadline(time.Now().Add(d))` перед каждым чтением закрывает простаивающие соединения. Число одновременных соединений ограничивают семафором (буферизованный канал). Остановка — закрыть listener: заблокированный `Accept` вернёт ошибку `net.ErrClosed`, цикл выйдет, а `sync.WaitGroup` дождётся обработчиков. Активные соединения при этом сами не закроются — их закрывают по сигналу (контекст, дедлайн «сейчас») или ждут.

## Как это в C#

- `TcpListener.AcceptTcpClientAsync` + `Task.Run` на клиента → `ln.Accept()` + `go handle(conn)`. Асинхронность в .NET явная (`async`/`await`, `ReadAsync`), в Go код синхронный, а ожидание сокета прячет рантайм.
- `NetworkStream` → `net.Conn`; `StreamReader.ReadLineAsync` → `bufio.Scanner` или `bufio.Reader.ReadString('\n')`; `BinaryPrimitives.ReadUInt32BigEndian` → `binary.BigEndian.Uint32`.
- System.IO.Pipelines (`PipeReader`, `SequenceReader`) решают кадрирование с минимумом копирований. В Go то же делают `bufio.Reader` и `io.ReadFull` поверх одного буфера; отдельной библиотеки нет.
- `Socket.ReceiveTimeout` задаётся один раз и действует только на синхронные чтения (для `ReadAsync` — `CancellationToken`); дедлайн в Go — абсолютный момент времени, и его продлевают перед каждым чтением. Отмену через `CancellationToken` заменяет `conn.SetDeadline(time.Now())` или `conn.Close()` из другой горутины.

## Ловушки

### Один `Read` — не одно сообщение

```go
buf := make([]byte, 4)
conn.Read(buf) // может прочитать 1, 2 или 3 байта — это не ошибка
n := binary.BigEndian.Uint32(buf)
```

`Read` возвращает столько, сколько уже пришло, — минимум 1 байт. Для «ровно N байт» есть `io.ReadFull(conn, buf)`: он вызывает `Read`, пока буфер не заполнится, и возвращает `io.ErrUnexpectedEOF`, если поток кончился посередине.

### Длина от клиента без лимита

```go
n := binary.BigEndian.Uint32(hdr)
payload := make([]byte, n) // клиент прислал 0xFFFFFFFF — 4 ГБ на одно соединение
```

Любое число из сети — недоверенные данные. Сравните длину с максимумом до выделения памяти. Для строк то же самое: у `bufio.Scanner` предел строки по умолчанию 64 КБ (`bufio.MaxScanTokenSize`), его меняют через `sc.Buffer`, а `Reader.ReadString` не ограничен вовсе.

### Новый `bufio.Reader` на каждое сообщение

```go
for {
	line, err := bufio.NewReader(conn).ReadString('\n') // буфер с прочитанным «впрок» выброшен
	// ...
}
```

`bufio.Reader` читает из соединения блоками по 4 КБ, и всё, что пришло после `\n`, остаётся в его буфере. Создайте один `Reader` (или `Scanner`) на соединение и читайте только через него.

## Задание

Напишите кодек кадров с префиксом длины:

```go
var ErrFrameTooLarge = errors.New("frame too large")

func WriteFrame(w io.Writer, payload []byte, max int) error
func ReadFrame(r io.Reader, max int) ([]byte, error)
```

Формат кадра: 4 байта длины данных — беззнаковое целое big-endian (`binary.BigEndian`), затем сами данные. Кадр `"abc"` — байты `00 00 00 03 61 62 63`. `max` всегда `>= 0`.

`WriteFrame`:

- если `len(payload) > max` — ошибка, для которой `errors.Is(err, ErrFrameTooLarge)`, и в `w` ничего не пишется;
- заголовок и данные пишутся **одним** вызовом `w.Write`;
- ошибка `w.Write` возвращается (так, чтобы `errors.Is` её находил).

`ReadFrame`:

- читает ровно один кадр и не читает из `r` ни байта сверх него (кадры идут в потоке подряд);
- `r` может отдавать данные кусками любого размера, вплоть до байта за вызов;
- если поток кончился до первого байта заголовка — возвращает `nil, io.EOF` (ровно `io.EOF`, без обёртки: так вызывающий отличает нормальный конец потока);
- если поток кончился внутри заголовка или данных — ошибка с `errors.Is(err, io.ErrUnexpectedEOF)`;
- если длина из заголовка больше `max` — ошибка с `errors.Is(err, ErrFrameTooLarge)`; данные кадра не читаются и память под них не выделяется;
- другая ошибка `r` возвращается (так, чтобы `errors.Is` её находил);
- при любой ошибке данные — `nil`; пустой кадр (длина 0) — срез `[]byte{}` (не `nil`) и `nil`-ошибка.
