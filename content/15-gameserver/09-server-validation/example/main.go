// Сервер не доверяет клиенту: каждый пакет из play проходит проверку
// в тике — числа конечны и в пределах мира, скорость не больше
// возможной, блок в пределах досягаемости, чат без управляющих
// символов, частота — в пределах бюджета. Время — номер тика,
// а не `time.Now`: проверка детерминирована и тестируется без часов.
package main

import (
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxCoord   = 3e7 // граница мира
	maxSpeed   = 1.0 // блоков за тик (бег — около 0.28, запас на лаги)
	maxCatchUp = 5   // сколько тиков «простоя» можно наверстать
	reach      = 6.0 // до центра блока (ванильный сервер 1.20.5+ — 4,5 до ближайшей точки блока)
	maxChat    = 256 // символов в сообщении
	chatBurst  = 3   // сообщений подряд
	chatEvery  = 20  // тиков на одно новое сообщение
)

type verdict string

// Ведро токенов на тиках: ёмкость `burst`, один токен каждые `every`
// тиков. Только целые числа и номер тика — никакой зависимости от
// времени машины, и пересчёт ленивый: при обращении, а не каждый тик.
type bucket struct {
	tokens, burst, every int
	last                 uint64 // тик последнего пополнения
}

func (b *bucket) take(tick uint64) bool {
	if n := int((tick - b.last) / uint64(b.every)); n > 0 {
		b.tokens = min(b.burst, b.tokens+n)
		b.last += uint64(n * b.every)
	}
	if b.tokens == 0 {
		return false
	}
	b.tokens--
	return true
}

type session struct {
	x, z float64 // последняя принятая позиция
	at   uint64  // тик, когда она принята
	chat bucket
}

// Проверка `!(d2 <= max2)` вместо `d2 > max2` — не стиль: любое
// сравнение с NaN ложно, и NaN прошёл бы проверку «больше максимума».
// Но NaN и ±Inf отсекаем ещё раньше — явно.
func (s *session) move(tick uint64, x, z float64) verdict {
	if !finite(x) || !finite(z) || math.Abs(x) > maxCoord || math.Abs(z) > maxCoord {
		return "отключить: координаты не число или за границей мира"
	}
	elapsed := float64(min(max(tick-s.at, 1), maxCatchUp))
	dx, dz := x-s.x, z-s.z
	if budget := maxSpeed * elapsed; !(dx*dx+dz*dz <= budget*budget) {
		return verdict(fmt.Sprintf("отклонить: moved too quickly, вернуть на (%.1f, %.1f)", s.x, s.z))
	}
	s.x, s.z, s.at = x, z, tick
	return "принять"
}

// Блок ставят и ломают только рядом: центр блока не дальше `reach`.
func (s *session) useBlock(bx, bz int) verdict {
	dx, dz := float64(bx)+0.5-s.x, float64(bz)+0.5-s.z
	if dx*dx+dz*dz > reach*reach {
		return "отклонить: слишком далеко"
	}
	return "принять"
}

// Чат: корректный UTF-8, длина в символах, без управляющих символов
// и без «§» — им Minecraft кодирует цвета, и клиенту его слать нельзя.
func (s *session) say(tick uint64, msg string) verdict {
	switch {
	case !utf8.ValidString(msg) || utf8.RuneCountInString(msg) > maxChat:
		return "отключить: битое или длинное сообщение"
	case strings.ContainsFunc(msg, func(r rune) bool { return unicode.IsControl(r) || r == '§' }):
		return "отключить: Illegal characters in chat"
	case !s.chat.take(tick):
		return "отклонить: слишком часто"
	}
	return "принять"
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func main() {
	s := &session{chat: bucket{tokens: chatBurst, burst: chatBurst, every: chatEvery}}
	show := func(tick uint64, what string, v verdict) { fmt.Printf("тик %3d  %-24s %s\n", tick, what, v) }

	show(1, "move 0.3, 0", s.move(1, 0.3, 0))
	show(2, "move 0.6, 0", s.move(2, 0.6, 0))
	show(3, "move 9, 0 (спидхак)", s.move(3, 9, 0))
	show(8, "move 4.6, 0 (после лага)", s.move(8, 4.6, 0)) // 6 тиков, засчитано 5
	show(9, "use 9, 0", s.useBlock(9, 0))
	show(9, "use 20, 0", s.useBlock(20, 0))

	for i, tick := range []uint64{10, 10, 11, 12, 30, 31} {
		show(tick, fmt.Sprintf("chat #%d", i+1), s.say(tick, "привет"))
	}
	show(40, "chat с \\n", s.say(40, "строка\nподделка"))
	show(41, "chat 300 символов", s.say(41, strings.Repeat("я", 300)))

	// NaN проходит наивную проверку скорости: NaN > max — false.
	nan := math.NaN()
	naive := nan*nan > maxSpeed*maxSpeed
	fmt.Println("наивная проверка видит NaN как спидхак:", naive)
	show(50, "move NaN, 0", s.move(50, nan, 0))
	show(51, "move 1e308, 0", s.move(51, 1e308, 0))
	show(52, "move +Inf, 0", s.move(52, math.Inf(1), 0))
}
