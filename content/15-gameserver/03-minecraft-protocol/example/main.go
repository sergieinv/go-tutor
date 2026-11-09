// Настоящие байты протокола Minecraft Java Edition 1.21.1 (протокол
// 767): так клиент открывает список серверов и входит в игру. Разбор
// и ответы — своим кодом, без библиотек: кадр, VarInt, строки,
// big-endian. В конце — сжатый кадр после Set Compression (zlib).
package main

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Handshake, как его шлёт клиент 1.21.1 при пинге `localhost:25565`:
// длина 0x10, id 0x00, протокол 767 (`ff 05`), адрес, порт `63 dd`,
// next state 1. Следом — Status Request: длина 1, id 0x00, полей нет.
var fromClient = []byte{
	0x10, 0x00, 0xff, 0x05, 0x09, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't', 0x63, 0xdd, 0x01,
	0x01, 0x00,
}

func main() {
	r := bytes.NewReader(fromClient)
	id, data := mustFrame(r)
	p := &fields{b: data}
	proto, addr, port, next := p.varint(), p.str(255), p.u16(), p.varint()
	fmt.Printf("handshake 0x%02x: протокол %d, %s:%d, next state %d\n", id, proto, addr, port, next)
	id, data = mustFrame(r)
	fmt.Printf("status request 0x%02x, полей: %d байт\n", id, len(data))

	// Status Response (0x00) — одна строка JSON. Клиент сравнивает
	// `protocol` со своим и показывает версию, онлайн и описание.
	js, _ := json.Marshal(map[string]any{
		"version":            map[string]any{"name": "1.21.1", "protocol": 767},
		"players":            map[string]any{"max": 20, "online": 0},
		"description":        map[string]any{"text": "Учебный сервер"},
		"enforcesSecureChat": false,
	})
	resp := appendFrame(nil, 0x00, appendStr(nil, string(js)))
	fmt.Printf("status response: кадр %d байт, начало % x\n", len(resp), resp[:6])
	fmt.Println(" ", string(js))

	// Ping (0x01) несёт int64; Pong (0x01) возвращает его как есть.
	ping := appendFrame(nil, 0x01, binary.BigEndian.AppendUint64(nil, 1731150000000))
	_, data = mustFrame(bytes.NewReader(ping))
	pong := appendFrame(nil, 0x01, data)
	fmt.Printf("ping % x\npong % x\n", ping, pong)

	// Вход. Login Start (0x00) с 1.20.2 несёт ник и UUID профиля;
	// offline-сервер UUID клиента не проверяет и считает свой:
	// MD5 от "OfflinePlayer:"+ник с версией 3.
	u := md5.Sum([]byte("OfflinePlayer:Steve"))
	u[6], u[8] = u[6]&0x0f|0x30, u[8]&0x3f|0x80
	// Login Success (0x02) в 767: UUID, ник, число свойств профиля
	// (у offline — 0) и Strict Error Handling — поле, которое было
	// только в 1.20.5–1.21.1.
	ls := append(u[:], appendStr(nil, "Steve")...)
	ls = append(appendVarInt(ls, 0), 0x00)
	fmt.Printf("login success: % x\n", appendFrame(nil, 0x02, ls))

	// После Login Acknowledged (0x03 от клиента) — configuration.
	// Сервер называет наборы данных, которые есть у него; клиент
	// отвечает, какие знает сам, — тогда в Registry Data можно не
	// присылать содержимое записей, клиент возьмёт своё.
	kp := appendVarInt(nil, 1)
	kp = appendStr(appendStr(appendStr(kp, "minecraft"), "core"), "1.21.1")
	fmt.Printf("known packs 0x0e: % x\n", appendFrame(nil, 0x0e, kp))
	for _, s := range []string{
		"S→C 0x0e Known Packs        ← C→S 0x07 Known Packs",
		"S→C 0x07 Registry Data      × по реестру: dimension_type, worldgen/biome, damage_type, …",
		"S→C 0x03 Finish Config      ← C→S 0x03 Acknowledge Finish → play",
		"S→C 0x2b Login (play)       → 0x22 Game Event 13, чанки 0x27, позиция 0x40",
	} {
		fmt.Println(" ", s)
	}

	// Set Compression (0x03 в login) с порогом 256: дальше у каждого
	// кадра после длины идёт VarInt «длина данных». 0 — не сжато,
	// иначе — размер id+данных до сжатия, а за ним zlib-поток.
	brand := appendStr(appendStr(nil, "minecraft:brand"), "gotutor") // plugin message 0x01
	big := appendStr(appendStr(nil, "gotutor:motd"), strings.Repeat("Добро пожаловать! ", 60))
	for _, pk := range []struct {
		name string
		body []byte
	}{{"brand", brand}, {"motd", big}} {
		packet := append(appendVarInt(nil, 0x01), pk.body...)
		frame := compressFrame(packet, 256)
		back, dataLen := decompressFrame(frame, 1<<20)
		fmt.Printf("%s: id+данные %d байт, длина данных в кадре %d, кадр короче: %v, распаковано то же: %v\n",
			pk.name, len(packet), dataLen, len(frame) < len(packet), bytes.Equal(back, packet))
	}
}

// compressFrame: меньше порога — `длина данных = 0` и пакет как есть,
// иначе — исходный размер и zlib.
func compressFrame(packet []byte, threshold int) []byte {
	var body []byte
	if len(packet) < threshold {
		body = append(appendVarInt(nil, 0), packet...)
	} else {
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		zw.Write(packet)
		zw.Close()
		body = append(appendVarInt(nil, int32(len(packet))), z.Bytes()...)
	}
	return append(appendVarInt(nil, int32(len(body))), body...)
}

// decompressFrame распаковывает не больше объявленного размера —
// и объявленный размер не больше лимита. Иначе zlib-бомба: мегабайт
// сжатых данных — гигабайт нулей на выходе (урок 09-security/10).
func decompressFrame(frame []byte, maxData int) ([]byte, int32) {
	r := bytes.NewReader(frame)
	n, _ := readVarInt(r)
	body := make([]byte, n)
	io.ReadFull(r, body)
	br := bytes.NewReader(body)
	dataLen, _ := readVarInt(br)
	if dataLen == 0 {
		return body[len(body)-br.Len():], 0
	}
	if dataLen < 0 || int(dataLen) > maxData {
		panic("слишком большой пакет")
	}
	zr, err := zlib.NewReader(br)
	if err != nil {
		panic(err)
	}
	out, _ := io.ReadAll(io.LimitReader(zr, int64(dataLen)+1))
	if len(out) != int(dataLen) {
		panic("размер не совпал с объявленным")
	}
	return out, dataLen
}

// Кадр, VarInt и поля — как в уроке 01, коротко.
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

func appendStr(b []byte, s string) []byte { return append(appendVarInt(b, int32(len(s))), s...) }

func appendFrame(b []byte, id int32, data []byte) []byte {
	body := append(appendVarInt(nil, id), data...)
	return append(appendVarInt(b, int32(len(body))), body...)
}

func mustFrame(r *bytes.Reader) (int32, []byte) {
	n, err := readVarInt(r)
	if err != nil || n < 1 || int(n) > r.Len() {
		panic("битый кадр")
	}
	body := make([]byte, n)
	io.ReadFull(r, body)
	br := bytes.NewReader(body)
	id, _ := readVarInt(br)
	return id, body[len(body)-br.Len():]
}

type fields struct{ b []byte }

func (f *fields) varint() int32 {
	r := bytes.NewReader(f.b)
	v, _ := readVarInt(r)
	f.b = f.b[len(f.b)-r.Len():]
	return v
}

func (f *fields) str(max int) string {
	n := int(f.varint())
	if n < 0 || n > max || n > len(f.b) {
		panic("плохая строка")
	}
	s := string(f.b[:n])
	f.b = f.b[n:]
	return s
}

func (f *fields) u16() uint16 {
	v := binary.BigEndian.Uint16(f.b)
	f.b = f.b[2:]
	return v
}
