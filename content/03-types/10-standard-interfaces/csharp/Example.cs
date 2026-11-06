using System.Text;

// ToString() есть у любого объекта — переопределяем
Console.WriteLine(new Money(12345, "RUB")); // 123.45 RUB

// Stream.CopyTo — аналог io.Copy, MemoryStream — аналог bytes.Buffer
using var src = new MemoryStream(Encoding.UTF8.GetBytes("hello, reader"));
using var dst = new MemoryStream();
src.CopyTo(dst);
Console.WriteLine(Encoding.UTF8.GetString(dst.ToArray()));

// Read тоже возвращает число прочитанных байт; конец — 0, а не ошибка EOF
var buf = new byte[3];
using var r = new MemoryStream(Encoding.UTF8.GetBytes("abcdefg"));
int n;
while ((n = r.Read(buf, 0, buf.Length)) > 0)
    Console.WriteLine(Encoding.UTF8.GetString(buf, 0, n));

// Comparison<T> — аналог slices.SortFunc
var prices = new List<Money> { new(300, "USD"), new(100, "USD") };
prices.Sort((x, y) => x.Cents.CompareTo(y.Cents));

record Money(long Cents, string Currency)
{
    public override string ToString() => $"{Cents / 100}.{Cents % 100:D2} {Currency}";
}
