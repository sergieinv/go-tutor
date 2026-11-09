// C#: та же схема — снапшот в тике, запись в Task.Run, атомарная
// замена файла. Флаг «идёт сохранение» — Interlocked, потому что
// тик и Task.Run живут на потоках пула и владельца состояния нет.
using System.Buffers.Binary;
using System.Runtime.InteropServices;

sealed class Chunk
{
    public ulong Gen;
    public ushort[] Blocks = new ushort[16 * 16];
    public Chunk CloneFor(ulong gen) => new() { Gen = gen, Blocks = (ushort[])Blocks.Clone() };
}

sealed record Snapshot(ulong Tick, KeyValuePair<(int X, int Z), Chunk>[] Chunks);

sealed class World
{
    readonly Dictionary<(int, int), Chunk> _chunks = new();
    ulong _gen;
    public ulong Tick;
    int _saving; // 0 или 1

    public void SetBlock(int x, int z, ushort b)
    {
        var key = (x >> 4, z >> 4); // сдвиг — floor и в C#
        if (!_chunks.TryGetValue(key, out var c))
            _chunks[key] = c = new Chunk { Gen = _gen };
        else if (c.Gen != _gen)
            _chunks[key] = c = c.CloneFor(_gen); // copy-on-write
        c.Blocks[(z & 15) * 16 + (x & 15)] = b;
    }

    // Вызывается из тика: снапшот — массив ссылок, запись — в фоне.
    public bool TryStartSave(string path)
    {
        if (Interlocked.CompareExchange(ref _saving, 1, 0) != 0)
            return false; // прошлое сохранение ещё пишется — пропуск
        var snap = new Snapshot(Tick, _chunks.OrderBy(kv => kv.Key).ToArray());
        _gen++;
        _ = Task.Run(async () =>
        {
            try { await WriteAtomicAsync(path, Encode(snap)); }
            finally { Volatile.Write(ref _saving, 0); }
        });
        return true;
    }

    static byte[] Encode(Snapshot s)
    {
        var buf = new byte[8 + s.Chunks.Length * 512];
        BinaryPrimitives.WriteUInt64BigEndian(buf, s.Tick);
        var off = 8;
        foreach (var (_, c) in s.Chunks)
            foreach (var b in c.Blocks) { BinaryPrimitives.WriteUInt16BigEndian(buf.AsSpan(off), b); off += 2; }
        return buf;
    }

    // Временный файл рядом, Flush(true) — аналог Sync, затем замена.
    static async Task WriteAtomicAsync(string path, byte[] data)
    {
        var tmp = path + ".tmp";
        await using (var fs = new FileStream(tmp, FileMode.Create, FileAccess.Write))
        {
            await fs.WriteAsync(data);
            fs.Flush(flushToDisk: true);
        }
        File.Move(tmp, path, overwrite: true);
    }
}

static class Program
{
    static void Main()
    {
        using var cts = new CancellationTokenSource();
        // Аналог signal.NotifyContext(ctx, SIGTERM) (.NET 6+).
        using var reg = PosixSignalRegistration.Create(PosixSignal.SIGTERM, ctx =>
        {
            ctx.Cancel = true; // не завершать процесс сразу
            cts.Cancel();
        });
        var world = new World();
        var path = Path.Combine(Path.GetTempPath(), "world.dat");
        world.SetBlock(-1, -1, 7);
        world.Tick = 4;
        Console.WriteLine(world.TryStartSave(path)); // True
    }
}
