package task

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
	// TODO: реализуйте функцию
	return nil
}
