using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;

// Пространственный хеш на C#: ключ ячейки — значимый кортеж (int, int)
// (IEquatable, без упаковки), списки — List<int>. В Go ключом map
// служит любая сравнимая структура, а хеш генерирует компилятор.
var grid = new SpatialHash(cellSize: 16);
var rng = new Random(7); // своё зерно — повторяемые данные
for (int id = 0; id < 10_000; id++)
    grid.Insert(id, rng.NextDouble() * 1024 - 512, rng.NextDouble() * 1024 - 512);

var found = new List<int>(capacity: 256); // буфер переиспользуется, как dst в Go
grid.QueryRadius(10, -20, 16, found);
Console.WriteLine($"найдено: {found.Count}, проверено: {grid.Checked} из 10000");

sealed class SpatialHash(double cellSize)
{
    private readonly Dictionary<(int X, int Z), List<int>> _cells = new();
    private readonly List<(double X, double Z, (int, int) Key, int Slot)> _ents = new();
    public int Checked { get; private set; }

    private (int, int) KeyOf(double x, double z) =>
        ((int)Math.Floor(x / cellSize), (int)Math.Floor(z / cellSize));

    public void Insert(int id, double x, double z)
    {
        var key = KeyOf(x, z);
        // Ссылка на значение в словаре без двойного поиска (.NET 6+).
        ref var list = ref CollectionsMarshal.GetValueRefOrAddDefault(_cells, key, out _);
        list ??= new List<int>();
        while (_ents.Count <= id) _ents.Add(default);
        _ents[id] = (x, z, key, list.Count);
        list.Add(id);
    }

    public void Remove(int id)
    {
        var e = _ents[id];
        var list = _cells[e.Key];
        int last = list[^1];
        list[e.Slot] = last; // поменять с последним и укоротить
        _ents[last] = _ents[last] with { Slot = e.Slot };
        list.RemoveAt(list.Count - 1);
        if (list.Count == 0) _cells.Remove(e.Key);
    }

    // УЯЗВИМО: огромный r даёт миллиарды ячеек (а при насыщении (int) — бесконечный
    // цикл: cx <= int.MaxValue всегда истинно). Ограничьте радиус или обходите
    // непустые ячейки, как в Go-примере.
    public void QueryRadius(double x, double z, double r, List<int> dst)
    {
        var (loX, loZ) = KeyOf(x - r, z - r);
        var (hiX, hiZ) = KeyOf(x + r, z + r);
        for (int cx = loX; cx <= hiX; cx++)
            for (int cz = loZ; cz <= hiZ; cz++)
            {
                if (!_cells.TryGetValue((cx, cz), out var list)) continue;
                foreach (int id in list)
                {
                    Checked++;
                    var (ex, ez, _, _) = _ents[id];
                    double dx = ex - x, dz = ez - z;
                    if (dx * dx + dz * dz <= r * r) dst.Add(id);
                }
            }
        dst.Sort();
    }
}
