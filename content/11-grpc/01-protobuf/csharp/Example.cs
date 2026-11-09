using Google.Protobuf;
using Google.Protobuf.WellKnownTypes;
using Shop.V1;

// Классы Order, Order.Types.Item и enum OrderStatus генерирует Grpc.Tools
// из того же orders.proto прямо при сборке:
//   <PackageReference Include="Google.Protobuf" Version="3.33.4" />
//   <PackageReference Include="Grpc.Tools" Version="2.71.0" PrivateAssets="All" />
//   <Protobuf Include="orders.proto" GrpcServices="None" />
// В Go тот же шаг — buf generate, а сгенерированный код коммитят.
var order = new Order
{
    Id = 9007199254740993,
    CustomerEmail = "ann@example.com",
    Status = OrderStatus.Paid, // префикс ORDER_STATUS_ генератор C# срезает
    Card = new Order.Types.CardPayment { Last4 = "4242" }, // oneof — просто свойство
    // Timestamp.FromDateTime требует DateTimeKind.Utc, иначе исключение.
    CreatedAt = Timestamp.FromDateTime(new DateTime(2026, 3, 1, 9, 30, 0, DateTimeKind.Utc)),
    ReserveFor = Duration.FromTimeSpan(TimeSpan.FromMinutes(15)),
};
// repeated и map — свойства только для чтения: RepeatedField<T> и MapField<K,V>.
order.Items.Add(new Order.Types.Item { Sku = "MUG-1", Quantity = 2, PriceCents = 450 });
order.Labels.Add("source", "app");

// oneof: какой вариант выбран — enum PaymentOneofCase, а не переключатель по типу.
Console.WriteLine(order.PaymentCase == Order.PaymentOneofCase.Card);

// optional: HasComment / ClearComment вместо указателя *string в Go.
order.Comment = "";
Console.WriteLine($"comment задан: {order.HasComment}");

// Бинарный вид: ToByteArray ↔ proto.Marshal, Parser.ParseFrom ↔ proto.Unmarshal.
byte[] wire = order.ToByteArray();
var back = Order.Parser.ParseFrom(wire);

// Equals сравнивает по значению (в Go — proto.Equal), == — по ссылке, как у *Order.
Console.WriteLine($"Equals: {order.Equals(back)}, ==: {ReferenceEquals(order, back)}");

// JsonFormatter ↔ protojson: те же правила — lowerCamelCase, int64 строкой.
var json = new JsonFormatter(JsonFormatter.Settings.Default.WithFormatDefaultValues(false));
Console.WriteLine(json.Format(order)); // {"id": "9007199254740993", "customerEmail": ...}

// Ручной разбор байтов, как protowire в Go: CodedInputStream читает теги.
var input = new CodedInputStream(new Order { Id = 150 }.ToByteArray());
uint tag = input.ReadTag();
Console.WriteLine($"поле {WireFormat.GetTagFieldNumber(tag)}, тип {WireFormat.GetTagWireType(tag)}, значение {input.ReadInt64()}");
