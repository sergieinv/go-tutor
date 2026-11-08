package task

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// item собирает тело заказа с одной позицией.
func item(fields string) string {
	return `{"customer":"Анна","items":[{` + fields + `}]}`
}

func wantInvalid(t *testing.T, name, body string) {
	t.Helper()
	o, total, err := parse(body)
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("%s: ошибка %v, want ErrInvalid", name, err)
	}
	if !reflect.DeepEqual(o, Order{}) || total != 0 {
		t.Errorf("%s: при ошибке вернулись %+v и %d, want Order{} и 0", name, o, total)
	}
}

func wantValid(t *testing.T, name, body string, wantTotal int64) {
	t.Helper()
	_, total, err := parse(body)
	if err != nil || total != wantTotal {
		t.Errorf("%s: (%d, %v), want (%d, nil)", name, total, err, wantTotal)
	}
}

// endless отдаёт начало JSON-строки, а потом бесконечно 'a'.
type endless struct {
	prefix string
	read   int64
}

func (e *endless) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if int(e.read) < len(e.prefix) {
			p[n] = e.prefix[e.read]
		} else {
			p[n] = 'a'
		}
		n++
		e.read++
	}
	return n, nil
}

type failingReader struct{}

var errDisk = errors.New("connection reset")

func (failingReader) Read([]byte) (int, error) { return 0, errDisk }

func TestExploitEndlessBody(t *testing.T) {
	// Клиент шлёт тело без конца: читать всё — потерять память.
	r := &endless{prefix: `{"customer":"Анна","note":"`}
	_, _, err := ParseOrder(r)
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("бесконечное тело: ошибка %v, want ErrTooLarge", err)
	}
	if r.read > MaxBody+1 {
		t.Errorf("прочитано %d байт, want не больше MaxBody+1 = %d", r.read, MaxBody+1)
	}
}

func TestExploitNegativeQty(t *testing.T) {
	// Отрицательная позиция уменьшает сумму заказа.
	wantInvalid(t, "qty < 0", `{"customer":"Анна","items":[
		{"sku":"TV-55","qty":1,"price_cents":9000000},
		{"sku":"CABLE","qty":-90,"price_cents":99990}]}`)
	wantInvalid(t, "price < 0", item(`"sku":"TV-55","qty":1,"price_cents":-9000000`))
}

func TestExploitOverflowTotal(t *testing.T) {
	// (2⁶² + 1) × 4 = 2⁶⁴ + 4 — в int64 это 4 копейки.
	wantInvalid(t, "переполнение", item(fmt.Sprintf(`"sku":"TV-55","qty":%d,"price_cents":4`, int64(1<<62+1))))
	wantInvalid(t, "огромная цена", item(fmt.Sprintf(`"sku":"TV-55","qty":2,"price_cents":%d`, int64(1<<62))))
}

func TestExploitExtraFields(t *testing.T) {
	// Поля, которых нет в контракте, — попытка что-то переопределить.
	wantInvalid(t, "total_cents", `{"customer":"Анна","total_cents":1,"items":[{"sku":"A","qty":1,"price_cents":100}]}`)
	wantInvalid(t, "discount в позиции", item(`"sku":"A","qty":1,"price_cents":100,"discount":100`))
}

func TestExploitSecondObject(t *testing.T) {
	// Второй объект после первого: другой парсер в цепочке может прочитать его.
	wantInvalid(t, "два объекта", item(`"sku":"A","qty":1,"price_cents":100`)+item(`"sku":"B","qty":1,"price_cents":1`))
	wantInvalid(t, "мусор после объекта", item(`"sku":"A","qty":1,"price_cents":100`)+` x`)
}

func TestExploitDuplicateKey(t *testing.T) {
	// encoding/json берёт последнее значение, ключ — без учёта регистра.
	wantInvalid(t, "дубль qty", item(`"sku":"A","qty":1,"QTY":-5,"price_cents":100`))
}

func TestTrailingWhitespaceOK(t *testing.T) {
	wantValid(t, "пробелы после объекта", item(`"sku":"A","qty":3,"price_cents":100`)+" \n\t ", 300)
}

func TestCaseInsensitiveKeys(t *testing.T) {
	wantValid(t, "ключи в другом регистре", `{"Customer":"Анна","ITEMS":[{"Sku":"A","Qty":2,"Price_Cents":5}]}`, 10)
}

func TestReadError(t *testing.T) {
	_, _, err := ParseOrder(failingReader{})
	if !errors.Is(err, errDisk) {
		t.Fatalf("ошибка чтения: %v, want обёрнутую ошибку reader", err)
	}
}

func TestExactlyMaxBody(t *testing.T) {
	body := item(`"sku":"A","qty":1,"price_cents":7`)
	body += strings.Repeat(" ", MaxBody-len(body)) // хвостовые пробелы допустимы
	if len(body) != MaxBody {
		t.Fatalf("тело %d байт", len(body))
	}
	wantValid(t, "ровно MaxBody", body, 7)
	if _, _, err := parse(body + " "); !errors.Is(err, ErrTooLarge) {
		t.Errorf("MaxBody+1 байт: ошибка %v, want ErrTooLarge", err)
	}
	if _, _, err := parse(strings.Repeat("{", MaxBody+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("большое тело с мусором: %v, want ErrTooLarge (размер проверяется первым)", err)
	}
}

func TestLimitsInRunes(t *testing.T) {
	one := `"items":[{"sku":"A","qty":1,"price_cents":1}]`
	wantValid(t, "customer 100 рун", `{"customer":"`+strings.Repeat("я", 100)+`",`+one+`}`, 1)
	wantInvalid(t, "customer 101 руна", `{"customer":"`+strings.Repeat("я", 101)+`",`+one+`}`)
	wantValid(t, "note 500 рун", `{"customer":"A","note":"`+strings.Repeat("ё", 500)+`",`+one+`}`, 1)
	wantInvalid(t, "note 501 руна", `{"customer":"A","note":"`+strings.Repeat("ё", 501)+`",`+one+`}`)
	wantValid(t, "пустой note", `{"customer":"A","note":"",`+one+`}`, 1)
}

func TestItemLimits(t *testing.T) {
	items := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `{"sku":"A","qty":1,"price_cents":2}`
		}
		return `{"customer":"A","items":[` + strings.Join(parts, ",") + `]}`
	}
	wantValid(t, "50 позиций", items(50), 100)
	wantInvalid(t, "51 позиция", items(51))

	wantValid(t, "граничные значения", item(`"sku":"`+strings.Repeat("Z", 32)+`","qty":1000,"price_cents":1000000000`), 1000*1000000000)
	wantValid(t, "нулевая цена", item(`"sku":"GIFT-0","qty":1,"price_cents":0`), 0)
	for name, fields := range map[string]string{
		"qty 1001":          `"sku":"A","qty":1001,"price_cents":1`,
		"цена 10⁹+1":        `"sku":"A","qty":1,"price_cents":1000000001`,
		"sku 33 байта":      `"sku":"` + strings.Repeat("Z", 33) + `","qty":1,"price_cents":1`,
		"пустой sku":        `"sku":"","qty":1,"price_cents":1`,
		"sku с пробелом":    `"sku":"A 1","qty":1,"price_cents":1`,
		"sku кириллицей":    `"sku":"КНИГА","qty":1,"price_cents":1`,
		"дробное qty":       `"sku":"A","qty":1.5,"price_cents":1`,
		"qty вне int64":     `"sku":"A","qty":9223372036854775808,"price_cents":1`,
		"qty null":          `"sku":"A","qty":null,"price_cents":1`,
		"нет qty":           `"sku":"A","price_cents":1`,
		"позиция-не-объект": `"sku":"A","qty":1,"price_cents":1},7,{"sku":"B","qty":1,"price_cents":1`,
	} {
		wantInvalid(t, name, item(fields))
	}
	// Отсутствующее поле — нулевое значение, а нулевая цена допустима.
	wantValid(t, "нет price_cents", item(`"sku":"A","qty":3`), 0)
}

func TestNullAndShapes(t *testing.T) {
	for name, body := range map[string]string{
		"null":              `null`,
		"строка":            `"order"`,
		"customer null":     `{"customer":null,"items":[{"sku":"A","qty":1,"price_cents":1}]}`,
		"items null":        `{"customer":"A","items":null}`,
		"items — объект":    `{"customer":"A","items":{"sku":"A"}}`,
		"нет items":         `{"customer":"A"}`,
		"незакрытый объект": `{"customer":"A","items":[{"sku":"A","qty":1,"price_cents":1}]`,
	} {
		wantInvalid(t, name, body)
	}
}
