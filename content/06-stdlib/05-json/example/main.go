// `encoding/json` сериализует структуры через рефлексию.
// Имена ключей и правила задают struct tags — строки
// после типа поля. Это аналог атрибутов `[JsonPropertyName]`.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// В JSON попадают только экспортируемые поля.
// `json:"name"` — имя ключа; `omitempty` — пропустить
// пустое значение (0, "", nil, пустые срез и map);
// `omitzero` (Go 1.24) — пропустить нулевое значение,
// в том числе нулевую структуру вроде `time.Time{}`;
// `-` — никогда не сериализовать; `string` — число
// в кавычках.
type user struct {
	ID       int64     `json:"id,string"`
	Name     string    `json:"name"`
	Email    string    `json:"email,omitempty"`
	Tags     []string  `json:"tags,omitempty"`
	Birthday time.Time `json:"birthday,omitzero"`
	Password string    `json:"-"`
	note     string    // неэкспортируемое — не видно пакету json
}

// Свой формат — метод `MarshalJSON` (и `UnmarshalJSON`).
type status int

func (s status) MarshalJSON() ([]byte, error) {
	return json.Marshal([]string{"active", "blocked"}[s])
}

func main() {
	u := user{ID: 42, Name: "Анна", Password: "secret", note: "x"}
	b, _ := json.Marshal(u)
	fmt.Println(string(b))

	// Разбор: ключи сопоставляются с тегами без учёта
	// регистра, лишние ключи молча игнорируются.
	var u2 user
	err := json.Unmarshal([]byte(`{"ID":"7","NAME":"Иван","age":30}`), &u2)
	fmt.Printf("%+v %v\n", u2, err)

	// В `map[string]any` все числа становятся `float64`:
	// большие целые теряют точность.
	var m map[string]any
	json.Unmarshal([]byte(`{"n": 9007199254740993, "ok": true}`), &m)
	fmt.Printf("%T %.0f %T\n", m["n"], m["n"], m["ok"])

	// `json.RawMessage` откладывает разбор: тело сообщения
	// разбираем, когда узнали его тип.
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	json.Unmarshal([]byte(`{"type":"user","data":{"name":"Олег"}}`), &env)
	fmt.Println(env.Type, string(env.Data))

	// `Decoder` читает поток. `DisallowUnknownFields`
	// превращает лишние ключи в ошибку — строгий режим.
	dec := json.NewDecoder(strings.NewReader(`{"name":"Вера","age":30}`))
	dec.DisallowUnknownFields()
	fmt.Println(dec.Decode(&u2))

	// `MarshalIndent` — читаемый вывод; свой `MarshalJSON`
	// вызывается для поля автоматически.
	b, _ = json.MarshalIndent(map[string]status{"anna": 0, "ivan": 1}, "", "  ")
	fmt.Println(string(b))

	// Encoder экранирует <, > и & для безопасной вставки
	// в HTML. Для API это обычно лишнее — отключается.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode("a<b")
	fmt.Print(buf.String())
}
