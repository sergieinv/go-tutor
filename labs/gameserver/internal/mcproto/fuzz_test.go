package mcproto

import (
	"bytes"
	"testing"
)

// FuzzFrameReader — произвольные байты не должны ронять декодер кадров
// (паника, бесконечный цикл, выделение памяти сверх предела) ни без
// сжатия, ни со сжатием. Запуск: go test -fuzz=FuzzFrameReader ./internal/mcproto
func FuzzFrameReader(f *testing.F) {
	pw := NewPacketWriter(16)
	f.Add(pw.Frame(nil, append(pw.Begin(0x06), bytes.Repeat([]byte("ab"), 40)...)), true)
	f.Add(pw.Frame(nil, pw.Begin(0x00)), true)
	f.Add(zipBomb(1000, 100000), true)
	f.Add([]byte{0x03, 0x00, 0x01, 0x02}, false)
	f.Add([]byte{0xff, 0xff, 0x7f}, false)
	f.Fuzz(func(t *testing.T, data []byte, compressed bool) {
		fr := NewFrameReader(bytes.NewReader(data), 4096)
		fr.MaxData = 8192
		if compressed {
			fr.SetCompression(16)
		}
		for i := 0; i < 64; i++ {
			_, p, err := fr.Next()
			if err != nil {
				return
			}
			if len(p) > 8192 {
				t.Fatalf("payload %d exceeds MaxData", len(p))
			}
		}
	})
}

// FuzzDecoder — разбор полей пакета из произвольных байт: только ошибки,
// никаких паник и чтения за границей.
func FuzzDecoder(f *testing.F) {
	f.Add(AppendString(AppendVarInt(nil, 767), "localhost"))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		d := NewDecoder(data)
		_ = d.VarInt()
		_ = d.String(255)
		_ = d.UShort()
		_, _, _ = d.Position()
		_ = d.Double()
		_ = d.Float()
		_ = d.UUID()
		_ = d.Long()
		_ = d.Bool()
		_ = d.Finish()
	})
}
