using Google.Protobuf;
using Google.Protobuf.WellKnownTypes;
using Shop.Current.V1;
using Shop.Legacy.V1;

// Классы сгенерированы Grpc.Tools из тех же order_legacy.proto и
// order_current.proto (<Protobuf Include="..." /> в .csproj).
// Правила совместимости задаёт формат, поэтому поведение — как в Go.

// Новый сервер -> старый клиент: tags и paid_at клиенту неизвестны.
var current = new CurrentOrder
{
    Id = 1,
    CustomerEmail = "ann@example.com",
    TotalCents = 1200,
    Status = CurrentStatus.Shipped,
    PaidAt = Timestamp.FromDateTime(new DateTime(2026, 3, 1, 10, 0, 0, DateTimeKind.Utc)),
};
current.Tags.Add("gift");

var onClient = LegacyOrder.Parser.ParseFrom(current.ToByteArray());
// Незнакомое значение enum — просто число в enum-типе C#: (LegacyStatus)3.
Console.WriteLine($"статус: {onClient.Status}, определён: {System.Enum.IsDefined(onClient.Status)}");

// Прокси на старом контракте меняет известное поле и пересылает дальше.
// Неизвестные поля (внутри — UnknownFieldSet) пишутся обратно.
onClient.TotalCents = 1000;
var forwarded = CurrentOrder.Parser.ParseFrom(onClient.ToByteArray());
Console.WriteLine($"после прокси: {string.Join(",", forwarded.Tags)} {forwarded.PaidAt}");

// JSON: переименование customer -> customer_email ломает разбор.
string json = JsonFormatter.Default.Format(new LegacyOrder { Id = 2, Customer = "bob@example.com" });
try
{
    JsonParser.Default.Parse<CurrentOrder>(json);
}
catch (InvalidProtocolBufferException e)
{
    Console.WriteLine($"JSON строго: {e.Message}");
}
// IgnoreUnknownFields ↔ DiscardUnknown в Go: ошибки нет, email потерян.
var lenient = new JsonParser(JsonParser.Settings.Default.WithIgnoreUnknownFields(true));
Console.WriteLine($"email: '{lenient.Parse<CurrentOrder>(json).CustomerEmail}'");

// Обработка статусов, которых клиент не знает: ветка по умолчанию обязательна.
string Describe(LegacyStatus s) => s switch
{
    LegacyStatus.New => "новый",
    LegacyStatus.Paid => "оплачен",
    _ => $"неизвестный статус {(int)s}",
};
Console.WriteLine(Describe(onClient.Status));
