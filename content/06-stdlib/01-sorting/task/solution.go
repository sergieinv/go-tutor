package task

import (
	"cmp"
	"slices"
)

// Player — результат игрока.
type Player struct {
	Name  string
	Score int // очки
	Time  int // время прохождения в секундах
}

// Leaderboard возвращает новый срез игроков, упорядоченный
// по очкам (по убыванию), затем по времени (по возрастанию),
// затем по имени. Исходный срез не меняется.
func Leaderboard(players []Player) []Player {
	// Копия: slices.SortFunc сортирует на месте.
	res := slices.Clone(players)
	slices.SortFunc(res, func(a, b Player) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score), // по убыванию
			cmp.Compare(a.Time, b.Time),
			cmp.Compare(a.Name, b.Name),
		)
	})
	return res
}
