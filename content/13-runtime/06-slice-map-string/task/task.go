package task

import "errors"

// Контрольные байты слота.
const (
	Empty   byte = 0x80 // слот пуст: поиск в этой группе можно заканчивать
	Deleted byte = 0xFE // надгробие: слот свободен, но поиск идёт дальше
)

// GroupSlots — слотов в группе.
const GroupSlots = 8

// ErrFull — в таблице не осталось ни пустых слотов, ни надгробий.
var ErrFull = errors.New("table is full")

// Table — хеш-таблица string → int с группами по 8 слотов.
// Поля выберите сами: например, срез групп, в каждой — контрольные
// байты, ключи и значения.
type Table struct {
	// TODO: поля таблицы
}

// New создаёт таблицу из numGroups групп (степень двойки ≥ 1).
func New(numGroups int, hash func(string) uint64) *Table {
	// TODO: проверьте numGroups, заполните контрольные байты значением Empty
	return &Table{}
}

// Get возвращает значение по ключу.
func (t *Table) Get(key string) (int, bool) {
	// TODO: пройдите по последовательности групп, сравнивая h2 и ключи
	return 0, false
}

// Put вставляет ключ или обновляет его значение.
func (t *Table) Put(key string, val int) error {
	// TODO: обновление, вставка в первое надгробие или первый пустой слот, ErrFull
	return nil
}

// Delete удаляет ключ: Empty, если в группе есть пустой слот, иначе Deleted.
func (t *Table) Delete(key string) bool {
	// TODO: реализуйте удаление
	return false
}

// Len возвращает число ключей.
func (t *Table) Len() int {
	// TODO: верните число ключей
	return 0
}

// Ctrl возвращает контрольные байты группы.
func (t *Table) Ctrl(group int) [GroupSlots]byte {
	// TODO: верните контрольные байты группы group
	return [GroupSlots]byte{}
}
