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

type group struct {
	ctrl [GroupSlots]byte
	keys [GroupSlots]string
	vals [GroupSlots]int
}

// Table — хеш-таблица string → int с группами по 8 слотов.
type Table struct {
	groups []group
	mask   uint64
	hash   func(string) uint64
	n      int
}

// New создаёт таблицу из numGroups групп (степень двойки ≥ 1).
func New(numGroups int, hash func(string) uint64) *Table {
	if numGroups < 1 || numGroups&(numGroups-1) != 0 {
		panic("task.New: numGroups must be a power of two")
	}
	t := &Table{groups: make([]group, numGroups), mask: uint64(numGroups - 1), hash: hash}
	for i := range t.groups {
		for j := range GroupSlots {
			t.groups[i].ctrl[j] = Empty
		}
	}
	return t
}

// split делит хеш на номер стартовой группы и h2.
func (t *Table) split(key string) (start uint64, h2 byte) {
	h := t.hash(key)
	return (h >> 7) & t.mask, byte(h & 0x7f)
}

// find ищет ключ и возвращает группу и слот.
func (t *Table) find(key string) (gi uint64, slot int, ok bool) {
	g, h2 := t.split(key)
	for i := range uint64(len(t.groups)) {
		if i > 0 {
			g = (g + i) & t.mask
		}
		grp := &t.groups[g]
		hasEmpty := false
		for j := range GroupSlots {
			switch c := grp.ctrl[j]; {
			case c == Empty:
				hasEmpty = true
			case c == h2 && grp.keys[j] == key:
				return g, j, true
			}
		}
		if hasEmpty {
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// Get возвращает значение по ключу.
func (t *Table) Get(key string) (int, bool) {
	g, j, ok := t.find(key)
	if !ok {
		return 0, false
	}
	return t.groups[g].vals[j], true
}

// Put вставляет ключ или обновляет его значение.
func (t *Table) Put(key string, val int) error {
	g, h2 := t.split(key)
	// Первое надгробие по пути и первый пустой слот группы, где поиск закончился.
	tombG, tombSlot := uint64(0), -1
	for i := range uint64(len(t.groups)) {
		if i > 0 {
			g = (g + i) & t.mask
		}
		grp := &t.groups[g]
		emptySlot := -1
		for j := range GroupSlots {
			switch c := grp.ctrl[j]; {
			case c == Empty:
				if emptySlot < 0 {
					emptySlot = j
				}
			case c == Deleted:
				if tombSlot < 0 {
					tombG, tombSlot = g, j
				}
			case c == h2 && grp.keys[j] == key:
				grp.vals[j] = val
				return nil
			}
		}
		if emptySlot >= 0 {
			// Ключа нет: дальше по последовательности его быть не может.
			if tombSlot >= 0 {
				t.store(tombG, tombSlot, h2, key, val)
			} else {
				t.store(g, emptySlot, h2, key, val)
			}
			return nil
		}
	}
	// Обошли все группы: пустых слотов нет, осталось только надгробие.
	if tombSlot >= 0 {
		t.store(tombG, tombSlot, h2, key, val)
		return nil
	}
	return ErrFull
}

func (t *Table) store(g uint64, j int, h2 byte, key string, val int) {
	grp := &t.groups[g]
	grp.ctrl[j] = h2
	grp.keys[j] = key
	grp.vals[j] = val
	t.n++
}

// Delete удаляет ключ. Надгробие ставится только в полной группе:
// пустой слот в ней оборвал бы поиск ключей, ушедших дальше.
func (t *Table) Delete(key string) bool {
	g, j, ok := t.find(key)
	if !ok {
		return false
	}
	grp := &t.groups[g]
	grp.keys[j] = ""
	grp.vals[j] = 0
	grp.ctrl[j] = Deleted
	for _, c := range grp.ctrl {
		if c == Empty {
			grp.ctrl[j] = Empty
			break
		}
	}
	t.n--
	return true
}

// Len возвращает число ключей.
func (t *Table) Len() int { return t.n }

// Ctrl возвращает контрольные байты группы.
func (t *Table) Ctrl(group int) [GroupSlots]byte { return t.groups[group].ctrl }
