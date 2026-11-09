package mcproto

import (
	"crypto/md5"
	"encoding/binary"
	"unicode/utf8"
)

// Теги NBT, которые нужны серверу. С 1.20.2 «сетевой» NBT пишется без
// имени корневого тега, а с 1.20.3 текстовые компоненты (чат, причина
// отключения) передаются в NBT, а не в JSON.
const (
	tagEnd       = 0
	tagString    = 8
	tagCompound  = 10
	tagLongArray = 12
)

// appendModifiedUTF8 пишет строку в «модифицированном UTF-8» Java: длина
// uint16 в байтах, NUL — два байта, символы вне BMP — суррогатной парой по
// 3 байта. Для кириллицы и ASCII совпадает с обычным UTF-8.
func appendModifiedUTF8(dst []byte, s string) []byte {
	lenAt := len(dst)
	dst = append(dst, 0, 0)
	for _, r := range s {
		switch {
		case r == 0:
			dst = append(dst, 0xC0, 0x80)
		case r == utf8.RuneError:
			dst = append(dst, "�"...)
		case r < 0x10000:
			dst = utf8.AppendRune(dst, r)
		default:
			r -= 0x10000
			hi, lo := 0xD800+(r>>10), 0xDC00+(r&0x3FF)
			dst = appendSurrogate(dst, hi)
			dst = appendSurrogate(dst, lo)
		}
		if len(dst)-lenAt-2 > 0xFFFF-6 { // предел длины строки NBT
			break
		}
	}
	binary.BigEndian.PutUint16(dst[lenAt:], uint16(len(dst)-lenAt-2))
	return dst
}

func appendSurrogate(dst []byte, c rune) []byte {
	return append(dst, byte(0xE0|(c>>12)), byte(0x80|((c>>6)&0x3F)), byte(0x80|(c&0x3F)))
}

// AppendTextPlain — текстовый компонент из одной строки (тег String).
func AppendTextPlain(dst []byte, s string) []byte {
	dst = append(dst, tagString)
	return appendModifiedUTF8(dst, s)
}

// AppendTextColored — компонент {text: s, color: color}.
func AppendTextColored(dst []byte, s, color string) []byte {
	dst = append(dst, tagCompound)
	dst = append(dst, tagString)
	dst = appendModifiedUTF8(dst, "text")
	dst = appendModifiedUTF8(dst, s)
	if color != "" {
		dst = append(dst, tagString)
		dst = appendModifiedUTF8(dst, "color")
		dst = appendModifiedUTF8(dst, color)
	}
	return append(dst, tagEnd)
}

// AppendHeightmaps — сетевой NBT карты высот: {MOTION_BLOCKING: long[]}.
func AppendHeightmaps(dst []byte, motionBlocking []int64) []byte {
	dst = append(dst, tagCompound)
	dst = append(dst, tagLongArray)
	dst = appendModifiedUTF8(dst, "MOTION_BLOCKING")
	dst = AppendInt(dst, int32(len(motionBlocking)))
	for _, v := range motionBlocking {
		dst = AppendLong(dst, v)
	}
	return append(dst, tagEnd)
}

// OfflineUUID — UUID игрока в offline-режиме, как у ванильного сервера:
// UUID версии 3 (MD5) от строки "OfflinePlayer:<имя>". Один и тот же ник
// всегда получает один и тот же UUID — на этом держатся сохранения.
func OfflineUUID(name string) [16]byte {
	u := md5.Sum([]byte("OfflinePlayer:" + name))
	u[6] = u[6]&0x0F | 0x30 // версия 3
	u[8] = u[8]&0x3F | 0x80 // вариант RFC 4122
	return u
}

// FormatUUID — каноническая запись 8-4-4-4-12.
func FormatUUID(u [16]byte) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 0, 36)
	for i, c := range u {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			b = append(b, '-')
		}
		b = append(b, hex[c>>4], hex[c&0xF])
	}
	return string(b)
}
