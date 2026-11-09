// C#: детерминированная симуляция упирается в те же места, что в Go:
// случайность, порядок обхода коллекций, время. Отличия — в деталях.
sealed record Input(ulong Tick, uint Player, int Dx, int Dz);

// TimeProvider (.NET 8) — внедряемые часы; FakeTimeProvider живёт
// в NuGet-пакете Microsoft.Extensions.TimeProvider.Testing,
// а простые ручные часы пишутся в пять строк.
sealed class ManualClock : TimeProvider
{
    DateTimeOffset _now = new(2026, 1, 1, 0, 0, 0, TimeSpan.Zero);
    public override DateTimeOffset GetUtcNow() => _now;
    public void Advance(TimeSpan d) => _now += d;
}

sealed class Sim
{
    // new Random(seed) повторим в пределах одной версии .NET, но
    // документация не обещает тот же поток чисел в следующих версиях.
    // Для журналов, которые переживут обновление, нужен свой ГПСЧ.
    readonly Random _rng;
    readonly SortedDictionary<uint, (int X, int Z)> _players = new(); // порядок по ключу
    readonly List<Input> _pending = new();
    public ulong Tick { get; private set; }

    public Sim(int seed) => _rng = new Random(seed);

    public void Submit(Input input) => _pending.Add(input);

    public void Step()
    {
        Tick++;
        // Канонический порядок внутри тика — как slices.SortFunc в Go.
        foreach (var input in _pending.Where(i => i.Tick == Tick)
                     .OrderBy(i => i.Player).ThenBy(i => i.Dx).ThenBy(i => i.Dz))
        {
            if (!_players.TryGetValue(input.Player, out var p))
                p = (_rng.Next(16), _rng.Next(16)); // вход
            _players[input.Player] = (p.X + input.Dx, p.Z + input.Dz);
        }
        _pending.RemoveAll(i => i.Tick <= Tick);
    }

    // string.GetHashCode() в .NET Core рандомизирован на процесс —
    // для хеша состояния он не годится, как и обход map в Go.
    public ulong Hash()
    {
        ulong h = 14695981039346656037; // FNV-1a
        void Mix(long v) { for (var i = 0; i < 8; i++) { h ^= (byte)(v >> (8 * i)); h *= 1099511628211; } }
        Mix((long)Tick);
        foreach (var (id, p) in _players) { Mix(id); Mix(p.X); Mix(p.Z); }
        return h;
    }
}

static class Program
{
    static ulong Run(int seed, bool reverse)
    {
        var sim = new Sim(seed);
        var inputs = Enumerable.Range(1, 5)
            .SelectMany(p => Enumerable.Range(1, 20).Select(t => new Input((ulong)t, (uint)p, t % 3 - 1, p % 3 - 1)))
            .ToList();
        if (reverse) inputs.Reverse(); // другой порядок прихода
        inputs.ForEach(sim.Submit);
        for (var i = 0; i < 20; i++) sim.Step();
        return sim.Hash();
    }

    static void Main()
    {
        Console.WriteLine(Run(42, false) == Run(42, true)); // True
        var clock = new ManualClock();
        clock.Advance(TimeSpan.FromMilliseconds(50)); // тик без ожидания
        Console.WriteLine(clock.GetUtcNow().Millisecond); // 50
    }
}
