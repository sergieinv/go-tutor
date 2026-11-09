// Пакет websocket — минимальная реализация RFC 6455 для шлюза
// браузерного клиента: рукопожатие (сервер и клиент), кадры, маскирование,
// фрагментация, ping/pong, close, лимит размера сообщения, проверка UTF-8.
//
// Расширений (permessage-deflate) и подпротоколов нет: шлюзу они не
// нужны, а каждая возможность — это код, который надо защищать от
// враждебного ввода. Почему не библиотека — см. README («Протокол
// WebSocket»).
package websocket

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"time"
	"unicode/utf8"
)

// Опкоды кадров.
const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
)

// Коды закрытия.
const (
	CloseNormal         = 1000
	CloseGoingAway      = 1001
	CloseProtocolError  = 1002
	CloseUnsupported    = 1003
	CloseInvalidPayload = 1007
	ClosePolicy         = 1008
	CloseTooBig         = 1009
)

// magic — GUID из RFC 6455 для Sec-WebSocket-Accept.
const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

var (
	ErrTooBig      = errors.New("websocket: message is too big")
	ErrProtocol    = errors.New("websocket: protocol error")
	ErrInvalidUTF8 = errors.New("websocket: invalid UTF-8 in text message")
	ErrRateLimited = errors.New("websocket: too many frames")
)

// CloseError — собеседник прислал close.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string { return fmt.Sprintf("websocket: closed %d %s", e.Code, e.Reason) }

// acceptKey — Sec-WebSocket-Accept для ключа клиента.
func acceptKey(key string) string {
	h := sha1.Sum([]byte(key + magic))
	return base64.StdEncoding.EncodeToString(h[:])
}

// Conn — соединение WebSocket. ReadMessage — из одной горутины, WriteMessage
// и Close — из любых (запись под мьютексом: ответ pong из горутины чтения
// не должен перемешаться с кадрами горутины записи).
type Conn struct {
	conn     net.Conn
	br       *bufio.Reader
	isServer bool

	// MaxMessage — предел размера сообщения (после сборки фрагментов).
	MaxMessage int
	// ReadTimeout — дедлайн на каждый кадр (0 — без дедлайна). Ставится
	// перед чтением каждого кадра, в том числе pong: живое соединение с
	// молчащим пользователем не обрывается, пока отвечает на ping.
	ReadTimeout time.Duration
	// WriteTimeout — дедлайн на запись каждого кадра, в том числе pong и
	// close, которые пишет горутина чтения. Без него клиент, который шлёт
	// ping и не читает ответы, навсегда заблокировал бы Write (а с ним —
	// слот соединения).
	WriteTimeout time.Duration
	// AllowFrame (если задан) вызывается на каждый входящий кадр, включая
	// управляющие и пустые фрагменты; false — close 1008 и ErrRateLimited.
	// Лимит на сообщения не ловит поток ping или фрагментов без конца.
	AllowFrame func() bool

	msg    []byte
	header [14]byte

	wmu    sync.Mutex
	wbuf   []byte
	closed bool
}

func newConn(c net.Conn, br *bufio.Reader, server bool) *Conn {
	return &Conn{conn: c, br: br, isServer: server, MaxMessage: 64 << 10, WriteTimeout: 10 * time.Second}
}

// NetConn — нижележащее соединение.
func (c *Conn) NetConn() net.Conn { return c.conn }

// readFrameHeader читает заголовок кадра.
func (c *Conn) readFrameHeader() (fin bool, op byte, length int64, mask [4]byte, masked bool, err error) {
	if c.ReadTimeout > 0 {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.ReadTimeout))
	}
	h := c.header[:2]
	if _, err = io.ReadFull(c.br, h); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	if h[0]&0x70 != 0 { // RSV1-3: расширений не согласовывали
		err = ErrProtocol
		return
	}
	op = h[0] & 0x0F
	masked = h[1]&0x80 != 0
	length = int64(h[1] & 0x7F)
	switch length {
	case 126:
		if _, err = io.ReadFull(c.br, c.header[2:4]); err != nil {
			return
		}
		length = int64(binary.BigEndian.Uint16(c.header[2:4]))
		if length < 126 { // не минимальная кодировка
			err = ErrProtocol
			return
		}
	case 127:
		if _, err = io.ReadFull(c.br, c.header[2:10]); err != nil {
			return
		}
		u := binary.BigEndian.Uint64(c.header[2:10])
		if u>>63 != 0 || u < 1<<16 {
			err = ErrProtocol
			return
		}
		length = int64(u)
	}
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}
	// Клиент обязан маскировать кадры, сервер — нет (RFC 6455, 5.1).
	if masked != c.isServer {
		err = ErrProtocol
	}
	return
}

func unmask(b []byte, mask [4]byte, offset int) {
	for i := range b {
		b[i] ^= mask[(offset+i)&3]
	}
}

// ReadMessage читает следующее сообщение с данными (text или binary),
// по дороге отвечая на ping и обрабатывая close. Срез действителен до
// следующего вызова.
func (c *Conn) ReadMessage() (op byte, data []byte, err error) {
	c.msg = c.msg[:0]
	msgOp := byte(0)
	for {
		fin, fop, n, mask, masked, err := c.readFrameHeader()
		if err != nil {
			return 0, nil, c.fail(err)
		}
		if c.AllowFrame != nil && !c.AllowFrame() {
			return 0, nil, c.fail(ErrRateLimited)
		}
		if fop >= 0x8 { // управляющий кадр
			if !fin || n > 125 {
				return 0, nil, c.fail(ErrProtocol)
			}
			var buf [125]byte
			p := buf[:n]
			if _, err := io.ReadFull(c.br, p); err != nil {
				return 0, nil, err
			}
			if masked {
				unmask(p, mask, 0)
			}
			switch fop {
			case OpPing:
				if err := c.writeFrame(OpPong, p); err != nil {
					return 0, nil, err
				}
			case OpPong:
			case OpClose:
				code, reason := CloseNormal, ""
				if len(p) >= 2 {
					code = int(binary.BigEndian.Uint16(p))
					if !validCloseCode(code) { // 1005, 1006, 1015 и т. п. по сети не ходят
						return 0, nil, c.fail(ErrProtocol)
					}
					if !utf8.Valid(p[2:]) {
						return 0, nil, c.fail(ErrInvalidUTF8)
					}
					reason = string(p[2:])
				} else if len(p) == 1 {
					return 0, nil, c.fail(ErrProtocol)
				}
				_ = c.WriteClose(code, "")
				return 0, nil, &CloseError{Code: code, Reason: reason}
			default:
				return 0, nil, c.fail(ErrProtocol)
			}
			continue
		}
		switch {
		case fop == OpContinuation && msgOp == 0: // продолжение без начала
			return 0, nil, c.fail(ErrProtocol)
		case fop != OpContinuation && msgOp != 0: // новое сообщение внутри фрагментированного
			return 0, nil, c.fail(ErrProtocol)
		case fop != OpContinuation && fop != OpText && fop != OpBinary:
			return 0, nil, c.fail(ErrProtocol)
		}
		if fop != OpContinuation {
			msgOp = fop
		}
		if int64(len(c.msg))+n > int64(c.MaxMessage) {
			return 0, nil, c.fail(ErrTooBig)
		}
		start := len(c.msg)
		c.msg = slices.Grow(c.msg, int(n))[:start+int(n)]
		if _, err := io.ReadFull(c.br, c.msg[start:]); err != nil {
			return 0, nil, err
		}
		if masked {
			unmask(c.msg[start:], mask, 0)
		}
		if fin {
			if msgOp == OpText && !utf8.Valid(c.msg) {
				return 0, nil, c.fail(ErrInvalidUTF8)
			}
			return msgOp, c.msg, nil
		}
	}
}

// validCloseCode — коды, которые можно прислать в кадре close (RFC 6455,
// 7.4): зарегистрированные 1000–1003, 1007–1014 и частные 3000–4999.
// 1004–1006 и 1015 зарезервированы и в сети недопустимы.
func validCloseCode(code int) bool {
	switch {
	case code >= 1000 && code <= 1003, code >= 1007 && code <= 1014, code >= 3000 && code <= 4999:
		return true
	}
	return false
}

// fail отвечает close с кодом по типу ошибки и возвращает ошибку.
func (c *Conn) fail(err error) error {
	switch {
	case errors.Is(err, ErrTooBig):
		_ = c.WriteClose(CloseTooBig, "message too big")
	case errors.Is(err, ErrInvalidUTF8):
		_ = c.WriteClose(CloseInvalidPayload, "invalid utf-8")
	case errors.Is(err, ErrProtocol):
		_ = c.WriteClose(CloseProtocolError, "protocol error")
	case errors.Is(err, ErrRateLimited):
		_ = c.WriteClose(ClosePolicy, "too many frames")
	}
	return err
}

// WriteMessage отправляет сообщение одним кадром.
func (c *Conn) WriteMessage(op byte, data []byte) error { return c.writeFrame(op, data) }

// WriteClose отправляет кадр close (один раз); дальше запись запрещена.
func (c *Conn) WriteClose(code int, reason string) error {
	if len(reason) > 123 {
		reason = reason[:123]
	}
	var p [125]byte
	binary.BigEndian.PutUint16(p[:], uint16(code))
	n := 2 + copy(p[2:], reason)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return nil
	}
	err := c.writeLocked(OpClose, p[:n])
	c.closed = true
	return err
}

func (c *Conn) writeFrame(op byte, data []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	return c.writeLocked(op, data)
}

func (c *Conn) writeLocked(op byte, data []byte) error {
	b := c.wbuf[:0]
	b = append(b, 0x80|op)
	maskBit := byte(0)
	if !c.isServer {
		maskBit = 0x80
	}
	switch n := len(data); {
	case n < 126:
		b = append(b, maskBit|byte(n))
	case n < 1<<16:
		b = append(b, maskBit|126)
		b = binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, maskBit|127)
		b = binary.BigEndian.AppendUint64(b, uint64(n))
	}
	if c.isServer {
		b = append(b, data...)
	} else {
		var mask [4]byte
		_, _ = rand.Read(mask[:])
		b = append(b, mask[:]...)
		start := len(b)
		b = append(b, data...)
		unmask(b[start:], mask, 0)
	}
	c.wbuf = b
	if c.WriteTimeout > 0 {
		_ = c.conn.SetWriteDeadline(time.Now().Add(c.WriteTimeout))
	}
	_, err := c.conn.Write(b)
	return err
}

// SetReadDeadline — абсолютный дедлайн чтения (например, на первое
// сообщение). Действует, пока ReadTimeout = 0.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// Close закрывает соединение без кадра close.
func (c *Conn) Close() error { return c.conn.Close() }
