// Ввод из-за границы доверия проверяют в три слоя: размер
// (сколько читать), форма (что это за JSON) и значения
// (диапазоны, длины). Здесь — как каждый слой обходят.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Transfer struct {
	To     string `json:"to"`
	Amount int64  `json:"amount"` // в копейках
}

const maxBody = 1 << 10

// Строгое чтение: не больше `maxBody` байт (в хендлере —
// `http.MaxBytesReader`), только известные поля, ровно один
// объект. `Decode` читает одно значение и на хвост не смотрит,
// поэтому после него ждём `io.EOF`.
func decodeStrict(r io.Reader, v any) error {
	data, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil {
		return err
	}
	if len(data) > maxBody {
		return errors.New("тело больше 1 КиБ")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("не объект Transfer")
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("данные после объекта")
	}
	return nil
}

// УЯЗВИМО: произведение переполняет `int64` молча — сумма
// заказа становится отрицательной или почти нулевой.
func totalUnsafe(qty, price int64) int64 { return qty * price }

// Границы проверяем до умножения: при `qty ≤ 1000` и цене
// до 10⁹ копеек произведение в `int64` поместится всегда.
func total(qty, price int64) (int64, error) {
	if qty < 1 || qty > 1000 || price < 0 || price > 1e9 {
		return 0, errors.New("вне диапазона")
	}
	return qty * price, nil
}

func main() {
	for _, c := range []struct{ name, body string }{
		{"обычный", `{"to":"bob","amount":500}`},
		{"лишнее поле", `{"to":"bob","amount":500,"fee":0}`},
		{"два объекта", `{"to":"bob","amount":5} {"to":"eve","amount":9000}`},
		{"большое тело", `{"to":"` + strings.Repeat("x", 2000) + `"}`},
		// Дубль ключа — не ошибка: побеждает последний, и имя
		// сравнивается без учёта регистра. Ловит его только
		// проверка значения.
		{"дубль ключа", `{"to":"bob","amount":5,"AMOUNT":-9000}`},
	} {
		var t Transfer
		err := decodeStrict(strings.NewReader(c.body), &t)
		if err == nil && t.Amount <= 0 {
			err = errors.New("amount должен быть > 0")
		}
		fmt.Printf("%-13s amount=%-6d %v\n", c.name, t.Amount, err)
	}

	// 2⁶² + 1 штук по 4 копейки: 2⁶⁴ + 4 по модулю 2⁶⁴ — это 4.
	qty := int64(1<<62 + 1)
	fmt.Println("без проверки:", totalUnsafe(qty, 4))
	_, err := total(qty, 4)
	fmt.Println("с проверкой:", err)

	// Усечение: `int32(x)` отбрасывает старшие биты без ошибки,
	// и id 4294967297 становится 1 — чужим объектом. Разбирайте
	// сразу в нужную битность: `ParseInt(s, 10, 32)`.
	id64, _ := strconv.ParseInt("4294967297", 10, 64)
	fmt.Println("int32(id):", int32(id64))
	_, err = strconv.ParseInt("4294967297", 10, 32)
	fmt.Println("ParseInt(…, 32):", errors.Is(err, strconv.ErrRange))

	// Длина: `len` — байты, лимиты для людей — в рунах. Строки
	// не из JSON (заголовки, query) могут быть не UTF-8 вовсе.
	name := "Ёжик"
	fmt.Println(len(name), utf8.RuneCountInString(name), utf8.ValidString("\xffadmin"))
}
