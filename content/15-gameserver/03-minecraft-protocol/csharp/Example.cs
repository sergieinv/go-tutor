// .NET: сжатие кадра Minecraft через ZLibStream (.NET 6+) — тот же формат
// zlib (RFC 1950), что compress/zlib в Go. Защита от zlib-бомбы одинакова:
// не распаковывать больше объявленного размера.
using System.IO.Compression;
using System.Text;

byte[] packet = Encoding.UTF8.GetBytes(string.Concat(Enumerable.Repeat("Добро пожаловать! ", 60)));

byte[] compressed;
using (var ms = new MemoryStream())
{
    using (var z = new ZLibStream(ms, CompressionLevel.Optimal))
        z.Write(packet);
    compressed = ms.ToArray();
}
Console.WriteLine($"{packet.Length} → {compressed.Length} байт");
Console.WriteLine(Encoding.UTF8.GetString(Inflate(compressed, packet.Length)) == Encoding.UTF8.GetString(packet));

// Бомба: 16 МиБ нулей, объявлено 1000 байт.
byte[] bomb;
using (var ms = new MemoryStream())
{
    using (var z = new ZLibStream(ms, CompressionLevel.Optimal))
        z.Write(new byte[16 << 20]);
    bomb = ms.ToArray();
}
try { Inflate(bomb, 1000); }
catch (InvalidDataException e) { Console.WriteLine(e.Message); }

// Читаем не больше declared + 1 байта: лишний байт — признак обмана.
// CopyTo/ReadToEnd без лимита распаковали бы бомбу целиком.
static byte[] Inflate(byte[] data, int declared)
{
    using var z = new ZLibStream(new MemoryStream(data), CompressionMode.Decompress);
    var buf = new byte[declared + 1];
    int n = 0, r;
    while (n < buf.Length && (r = z.Read(buf, n, buf.Length - n)) > 0) n += r;
    if (n != declared) throw new InvalidDataException("размер не совпал с объявленным");
    return buf[..declared];
}
