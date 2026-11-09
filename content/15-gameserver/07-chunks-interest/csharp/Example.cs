using System;
using System.Collections.Generic;
using System.Linq;

// Тот же interest management по-сишарповски: чанк — record struct
// (равенство и хеш по значению, как у структуры-ключа map в Go),
// квадраты — HashSet, разность — ExceptWith. Это проще читать,
// но каждый переход границы выделяет два множества; в Go-примере
// разность считается двумя вложенными циклами без аллокаций множеств.
var world = new World();
world.Join("alice", 8, 8, r: 2);
var (load, unload) = world.Move("alice", 16.1, 8);
Console.WriteLine($"загрузить {load.Count}, выгрузить {unload.Count}"); // 5, 5

// Целочисленное деление и приведение double → int в C# тоже
// усекают к нулю: (int)(-0.5) / 16 == 0. Нужен Math.Floor.
Console.WriteLine(World.ChunkOf(-0.5, 0)); // ChunkPos { X = -1, Z = 0 }
// >> у int в C# арифметический, как в Go: -1 >> 4 == -1.
Console.WriteLine(-1 >> 4);

readonly record struct ChunkPos(int X, int Z);

sealed class World
{
    private readonly Dictionary<string, (ChunkPos Center, int R)> _players = new();
    // Обратный индекс «чанк → подписчики».
    private readonly Dictionary<ChunkPos, HashSet<string>> _subs = new();

    public static ChunkPos ChunkOf(double x, double z) =>
        new((int)Math.Floor(x / 16), (int)Math.Floor(z / 16));

    private static HashSet<ChunkPos> Square(ChunkPos c, int r)
    {
        var set = new HashSet<ChunkPos>();
        for (int x = c.X - r; x <= c.X + r; x++)
            for (int z = c.Z - r; z <= c.Z + r; z++)
                set.Add(new ChunkPos(x, z));
        return set;
    }

    public void Join(string name, double x, double z, int r)
    {
        var c = ChunkOf(x, z);
        _players[name] = (c, r);
        Subscribe(name, Square(c, r));
    }

    public (List<ChunkPos> Load, List<ChunkPos> Unload) Move(string name, double x, double z)
    {
        var (old, r) = _players[name];
        var now = ChunkOf(x, z);
        if (now == old) return ([], []);
        var load = Square(now, r);
        load.ExceptWith(Square(old, r));
        var unload = Square(old, r);
        unload.ExceptWith(Square(now, r));
        foreach (var c in unload)
            if (_subs.TryGetValue(c, out var s) && s.Remove(name) && s.Count == 0)
                _subs.Remove(c);
        Subscribe(name, load);
        _players[name] = (now, r);
        return (load.ToList(), unload.ToList());
    }

    private void Subscribe(string name, IEnumerable<ChunkPos> chunks)
    {
        foreach (var c in chunks)
        {
            if (!_subs.TryGetValue(c, out var s)) _subs[c] = s = new HashSet<string>();
            s.Add(name);
        }
    }

    // Получатели события «блок изменён» — подписчики чанка.
    public IReadOnlyCollection<string> BlockChanged(int bx, int bz) =>
        _subs.TryGetValue(new ChunkPos(bx >> 4, bz >> 4), out var s) ? s : Array.Empty<string>();
}
