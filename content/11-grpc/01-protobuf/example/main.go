// Контракт лежит рядом — `orders.proto`. Песочница перед
// каждым запуском выполняет `buf generate`, и рядом
// появляется `orders.pb.go`: структуры `Order`, `Order_Item`,
// тип `OrderStatus` и дескриптор файла. Пример собирает заказ,
// смотрит на его байты и JSON.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func main() {
	// Начнём с маленького сообщения, чтобы разобрать байты
	// руками. `proto.Marshal` пишет только заданные поля:
	// у каждого — тег (номер поля и wire type), затем значение.
	small := &Order{Id: 150, CustomerEmail: "a@b.c"}
	b, err := proto.Marshal(small)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("байты: % x\n", b)

	// Тег — varint `номер<<3 | wire type`: `08` — поле 1,
	// тип 0 (varint), `12` — поле 2, тип 2 (длина + байты).
	// 150 в varint — `96 01`: по 7 бит, старший бит — «дальше
	// есть ещё байт». Имён полей в байтах нет — только номера.
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			log.Fatal(protowire.ParseError(n))
		}
		b = b[n:]
		switch typ {
		case protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			fmt.Printf("  поле %d varint %d\n", num, v)
			b = b[n:]
		case protowire.BytesType:
			v, n := protowire.ConsumeBytes(b)
			fmt.Printf("  поле %d bytes %q\n", num, v)
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			b = b[n:]
		}
	}

	// Нулевые значения на провод не попадают: пустой заказ —
	// ноль байт. У поля без `optional` «не задано» и «ноль»
	// неотличимы. `optional` даёт presence: в Go поле —
	// указатель, и пустая строка, заданная явно, кодируется.
	empty := &Order{Status: OrderStatus_ORDER_STATUS_UNSPECIFIED}
	withComment := &Order{Comment: proto.String("")}
	fmt.Println("размер пустого:", proto.Size(empty),
		"с comment=\"\":", proto.Size(withComment),
		"comment задан:", withComment.Comment != nil)

	// Полный заказ: вложенные сообщения, `repeated`, `map`,
	// `oneof` и well-known types. Вариант `oneof` — отдельный
	// тип-обёртка `Order_Card`, его кладут в поле `Payment`.
	created := time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)
	order := &Order{
		Id:            9007199254740993,
		CustomerEmail: "ann@example.com",
		Status:        OrderStatus_ORDER_STATUS_PAID,
		Items: []*Order_Item{
			{Sku: "MUG-1", Quantity: 2, PriceCents: 450},
		},
		Labels:     map[string]string{"source": "app", "gift": "yes"},
		Payment:    &Order_Card{Card: &Order_CardPayment{Last4: "4242"}},
		CreatedAt:  timestamppb.New(created),
		ReserveFor: durationpb.New(15 * time.Minute),
	}

	// Какой вариант `oneof` выбран — переключатель по типу.
	switch p := order.GetPayment().(type) {
	case *Order_Card:
		fmt.Println("оплата картой *" + p.Card.GetLast4())
	case *Order_CashOnDelivery:
		fmt.Println("оплата при получении")
	case nil:
		fmt.Println("способ оплаты не выбран")
	}

	// Порядок записей `map` в байтах не определён: два
	// `Marshal` одного сообщения могут дать разные байты.
	// Для подписи, кеш-ключа или сравнения — `Deterministic`.
	det := proto.MarshalOptions{Deterministic: true}
	wire, err := det.Marshal(order)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("бинарный размер:", len(wire))

	// `protojson` — каноническое JSON-отображение proto3:
	// имена в lowerCamelCase, `int64` — строкой (JavaScript
	// теряет точность после 2^53), enum — именем, `Timestamp`
	// — RFC 3339, `Duration` — секунды с суффиксом `s`.
	// Пробелы в выводе `protojson` намеренно нестабильны,
	// поэтому для печати прогоняем через `json.Indent`.
	js, err := protojson.Marshal(order)
	if err != nil {
		log.Fatal(err)
	}
	printJSON(js)

	// По умолчанию нулевые поля в JSON не попадают.
	// `EmitUnpopulated` выводит их со значениями по умолчанию;
	// `oneof` и не заданные `optional` всё равно опускаются.
	js, err = protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(&Order{Id: 7})
	if err != nil {
		log.Fatal(err)
	}
	printJSON(js)

	// Обратно: байты → новое сообщение. Сравнивают
	// сообщения `proto.Equal`, а не `==` (это указатели)
	// и не `reflect.DeepEqual` (внутреннее состояние).
	var back Order
	if err := proto.Unmarshal(wire, &back); err != nil {
		log.Fatal(err)
	}
	fmt.Println("proto.Equal:", proto.Equal(order, &back), "==:", order == &back)
}

func printJSON(js []byte) {
	var out bytes.Buffer
	if err := json.Indent(&out, js, "", "  "); err != nil {
		log.Fatal(err)
	}
	fmt.Println(out.String())
}
