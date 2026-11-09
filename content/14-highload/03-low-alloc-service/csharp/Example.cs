using System.Buffers;
using System.Buffers.Text;
using System.Text;
using System.Text.Json;

var order = new Order(42, "Анна", [new Item("A-1", 2, 1999), new Item("B-7", 1, 54900)]);

// «До»: анонимный объект и сериализация через рефлексию —
// ближайший аналог map[string]any + json.NewEncoder(w).Encode.
string slow = JsonSerializer.Serialize(new { count = order.Items.Length, order });
Console.WriteLine(slow.Length > 0);

// «После»: Utf8JsonWriter пишет прямо в IBufferWriter без промежуточных
// строк. ArrayBufferWriter — обычный растущий массив, не пул (пул —
// ArrayPool ниже, ≈ sync.Pool). Кириллицу writer по умолчанию
// экранирует (\u0410…), encoding/json в Go — нет.
var buffer = new ArrayBufferWriter<byte>(4096);
using (var w = new Utf8JsonWriter(buffer))
{
    w.WriteStartObject();
    w.WriteNumber("count", order.Items.Length);
    w.WriteStartObject("order");
    w.WriteNumber("id", order.Id);
    w.WriteString("customer", order.Customer);
    w.WriteEndObject();
    w.WriteEndObject();
}
Console.WriteLine(Encoding.UTF8.GetString(buffer.WrittenSpan));

// Utf8Formatter.TryFormat ≈ strconv.AppendInt: число пишется
// в чужой буфер, без строки и без упаковки.
byte[] rented = ArrayPool<byte>.Shared.Rent(64);
try
{
    Utf8Formatter.TryFormat(order.Id, rented, out int written);
    Console.WriteLine(Encoding.ASCII.GetString(rented, 0, written));
}
finally
{
    ArrayPool<byte>.Shared.Return(rented);
}

// Предразмер: как make([]T, 0, n) и make(map[K]V, n).
var list = new List<Item>(capacity: 1000);
var dict = new Dictionary<long, int>(capacity: 1000);
Console.WriteLine($"{list.Capacity} {dict.EnsureCapacity(0) >= 1000}");

record Item(string Sku, int Qty, long Price);
record Order(long Id, string Customer, Item[] Items);
