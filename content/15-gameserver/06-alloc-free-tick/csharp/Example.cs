// Тик без аллокаций на .NET: буферы живут между тиками, числа пишутся
// BinaryPrimitives в Span<byte>, буферы для передачи писателю — из
// ArrayPool. Проверка «ноль выделений» — GC.GetAllocatedBytesForCurrentThread,
// аналог testing.AllocsPerRun.
using System.Buffers;
using System.Buffers.Binary;
using System.Runtime;

public struct Entity
{
    public int Id;
    public double X, Z, VX, VZ;
}

public sealed class Viewer
{
    public double X, Z, Radius;
    public byte[] Out = new byte[256];
    public int OutLen;
}

public sealed class World
{
    public Entity[] Entities = [];
    public Viewer[] Viewers = [];
    private byte[] _body = new byte[256]; // черновик тела, переживает тики

    // VarInt — то же, что BinaryWriter.Write7BitEncodedInt, но в Span.
    private static int WriteVarInt(Span<byte> dst, int value)
    {
        var u = (uint)value;
        var i = 0;
        while (u >= 0x80) { dst[i++] = (byte)(u | 0x80); u >>= 7; }
        dst[i++] = (byte)u;
        return i;
    }

    private static void Ensure(ref byte[] buf, int need)
    {
        if (buf.Length < need) Array.Resize(ref buf, Math.Max(need, buf.Length * 2)); // только на прогреве
    }

    public void Tick()
    {
        foreach (ref var e in Entities.AsSpan()) // ref — без копии структуры
        {
            e.X += e.VX;
            e.Z += e.VZ;
        }
        Span<byte> head = stackalloc byte[10]; // stackalloc — вне цикла (CA2014)
        foreach (var v in Viewers)
        {
            var r2 = v.Radius * v.Radius;
            Ensure(ref _body, 10 + Entities.Length * 21);
            var n = 0;
            var p = 10; // место под id и count — запишем позже
            foreach (ref readonly var e in Entities.AsSpan())
            {
                double dx = e.X - v.X, dz = e.Z - v.Z;
                if (dx * dx + dz * dz > r2) continue;
                n++;
                p += WriteVarInt(_body.AsSpan(p), e.Id);
                BinaryPrimitives.WriteDoubleBigEndian(_body.AsSpan(p), e.X); p += 8;
                BinaryPrimitives.WriteDoubleBigEndian(_body.AsSpan(p), e.Z); p += 8;
            }
            var h = WriteVarInt(head, 0x20);
            h += WriteVarInt(head[h..], n);
            var bodyLen = h + (p - 10);
            Ensure(ref v.Out, bodyLen + 5);
            var o = WriteVarInt(v.Out, bodyLen);
            head[..h].CopyTo(v.Out.AsSpan(o)); o += h;
            _body.AsSpan(10, p - 10).CopyTo(v.Out.AsSpan(o));
            v.OutLen = o + p - 10;
        }
    }
}

public static class Program
{
    public static void Main()
    {
        // Режим GC под задержки: в Go вместо этого GOGC и GOMEMLIMIT.
        GCSettings.LatencyMode = GCLatencyMode.SustainedLowLatency;

        var w = new World
        {
            Entities = Enumerable.Range(0, 500).Select(i => new Entity { Id = i, X = i % 50, Z = i / 50 }).ToArray(),
            Viewers = Enumerable.Range(0, 20).Select(i => new Viewer { X = i * 3, Z = 5, Radius = 12 }).ToArray(),
        };
        w.Tick(); // прогрев: буферы доросли до нужного размера
        var before = GC.GetAllocatedBytesForCurrentThread();
        for (var i = 0; i < 100; i++) w.Tick();
        // Сначала посчитать, потом печатать: интерполяция строки сама выделяет память.
        var allocated = GC.GetAllocatedBytesForCurrentThread() - before;
        Console.WriteLine($"байт выделено за 100 тиков: {allocated}");

        // Передача кадра писателю — арендованный буфер; писатель вернёт его.
        var frame = ArrayPool<byte>.Shared.Rent(w.Viewers[0].OutLen);
        w.Viewers[0].Out.AsSpan(0, w.Viewers[0].OutLen).CopyTo(frame);
        ArrayPool<byte>.Shared.Return(frame);
    }
}
