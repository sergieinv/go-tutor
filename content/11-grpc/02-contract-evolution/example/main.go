// Две версии одного контракта лежат рядом: `order_legacy.proto`
// — с ним собран старый клиент, `order_current.proto` — новый
// сервер. Между версиями поле переименовали, одно удалили
// (с `reserved`), два добавили и расширили enum. Пример гоняет
// байты между версиями в обе стороны и показывает, что
// выдерживает бинарный формат, а что ломается.

package main

import (
	"fmt"
	"log"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func main() {
	// Старый клиент → новый сервер. Сервер не знает поля 4
	// (`promo_code` удалён), но разбор не падает: поле
	// попадает в «неизвестные» и хранится в сообщении как есть.
	// Переименование `customer` → `customer_email` по проводу
	// незаметно: идёт номер 2, а не имя.
	legacy := &LegacyOrder{
		Id: 1, Customer: "ann@example.com", TotalCents: 1200,
		PromoCode: "SPRING", Status: LegacyStatus_LEGACY_STATUS_PAID,
	}
	var onServer CurrentOrder
	mustUnmarshal(mustMarshal(legacy), &onServer)
	fmt.Println("сервер:", onServer.GetCustomerEmail(), onServer.GetTotalCents(), onServer.GetStatus())
	fmt.Printf("  неизвестные байты: % x\n", onServer.ProtoReflect().GetUnknown())

	// Новый сервер → старый клиент. `tags` и `paid_at` клиенту
	// неизвестны — тоже в неизвестные. Значение enum 3
	// (`SHIPPED`) клиент не знает, но в proto3 enum открытый:
	// число сохраняется, `String()` печатает его цифрой.
	current := &CurrentOrder{
		Id: 1, CustomerEmail: "ann@example.com", TotalCents: 1200,
		Status: CurrentStatus_CURRENT_STATUS_SHIPPED,
		Tags:   []string{"gift"},
		PaidAt: timestamppb.New(time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)),
	}
	var onClient LegacyOrder
	mustUnmarshal(mustMarshal(current), &onClient)
	fmt.Println("клиент: статус", onClient.GetStatus(),
		"неизвестных байт:", len(onClient.ProtoReflect().GetUnknown()))

	// Пересылка через старый узел. Прокси со старым контрактом
	// меняет известное ему поле и отправляет дальше. Неизвестные
	// поля `Marshal` пишет обратно, так что новые данные
	// доходят до адресата целыми.
	onClient.TotalCents = 1000
	var forwarded CurrentOrder
	mustUnmarshal(mustMarshal(&onClient), &forwarded)
	fmt.Println("после прокси:", forwarded.GetTags(), forwarded.GetPaidAt().AsTime().Format(time.DateOnly),
		forwarded.GetStatus(), forwarded.GetTotalCents())

	// А вот JSON переименование ломает: в JSON идут имена.
	// Сервер отвергает незнакомое поле `customer`, а с
	// `DiscardUnknown` молча теряет email — это ещё хуже.
	js, err := protojson.Marshal(&LegacyOrder{Id: 2, Customer: "bob@example.com"})
	if err != nil {
		log.Fatal(err)
	}
	var fromJSON CurrentOrder
	err = protojson.Unmarshal(js, &fromJSON)
	fmt.Println("JSON строго:", err != nil)
	err = protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(js, &fromJSON)
	fmt.Printf("JSON с DiscardUnknown: err=%v email=%q\n", err, fromJSON.GetCustomerEmail())

	// Смена типа поля — несовместимое изменение. Допустим,
	// кто-то объявил `total_cents` строкой и прислал поле 3
	// с wire type «длина + байты». Сервер ждёт varint: ошибки
	// нет, поле уходит в неизвестные, сумма — тихий ноль.
	wire := protowire.AppendTag(nil, 3, protowire.BytesType)
	wire = protowire.AppendString(wire, "1200")
	var broken CurrentOrder
	mustUnmarshal(wire, &broken)
	fmt.Println("тип сменили: total_cents =", broken.GetTotalCents(),
		"неизвестных байт:", len(broken.ProtoReflect().GetUnknown()))

	// Смена ширины числа: `int64`, `int32`, `uint64` и enum
	// кодируются одинаково (varint), но получатель с 32-битным
	// полем молча отбрасывает старшие биты. Пусть в поле 5
	// пришло 2^32+2 от версии, где оно было `int64`: старый
	// клиент читает enum и видит 2 — `PAID`, которого не было.
	wire = protowire.AppendTag(nil, 5, protowire.VarintType)
	wire = protowire.AppendVarint(wire, 1<<32+2)
	var truncated LegacyOrder
	mustUnmarshal(wire, &truncated)
	fmt.Println("усечение:", truncated.GetStatus())
}

func mustMarshal(m proto.Message) []byte {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		log.Fatal(err)
	}
	return b
}

func mustUnmarshal(b []byte, m proto.Message) {
	if err := proto.Unmarshal(b, m); err != nil {
		log.Fatal(err)
	}
}
