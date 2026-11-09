//go:build race

package world

// raceEnabled: под -race sync.Pool нарочно теряет часть объектов, и
// счётчик аллокаций врёт — проверку аллокаций пропускаем.
const raceEnabled = true
