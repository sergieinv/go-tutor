// Игровой цикл на .NET: состояние мира принадлежит одной задаче,
// команды приходят через ограниченный Channel<T>, тик — по сетке
// с догонянием. В Go то же самое — горутина, chan и select с default.
using System.Diagnostics;
using System.Threading.Channels;

public interface ICommand { void Apply(World w); }

public sealed record Join(string Name) : ICommand
{
    public void Apply(World w) => w.Players[Name] = (0, 0);
}

public sealed class World
{
    // Словарь без lock: его трогает только цикл мира.
    public readonly Dictionary<string, (double X, double Z)> Players = new();
    public ulong Tick;

    // Ограниченный вход: TryWrite не ждёт — как select с default.
    private readonly Channel<ICommand> _cmds = Channel.CreateBounded<ICommand>(
        new BoundedChannelOptions(1024) { SingleReader = true, FullMode = BoundedChannelFullMode.Wait });

    public bool Submit(ICommand c) => _cmds.Writer.TryWrite(c);

    public void Step()
    {
        Tick++;
        for (var i = 0; i < 64 && _cmds.Reader.TryRead(out var c); i++)
            c.Apply(this);
        // ... обновить мир, разослать изменения
    }
}

public static class Program
{
    public static async Task Main()
    {
        var world = new World();
        world.Submit(new Join("alice"));

        // Накопитель на Stopwatch: тики по сетке, догонять не больше трёх.
        var interval = TimeSpan.FromMilliseconds(50);
        const int maxCatchUp = 3;
        var clock = Stopwatch.StartNew();
        var next = clock.Elapsed;
        long skipped = 0;
        while (world.Tick < 5)
        {
            var due = clock.Elapsed >= next ? (int)((clock.Elapsed - next) / interval) + 1 : 0;
            if (due > maxCatchUp)
            {
                skipped += due - maxCatchUp;
                next += (due - maxCatchUp) * interval;
                due = maxCatchUp;
            }
            for (var i = 0; i < due; i++)
            {
                var start = clock.Elapsed;
                world.Step();
                var mspt = (clock.Elapsed - start).TotalMilliseconds; // MSPT тика
                next += interval;
            }
            var wait = next - clock.Elapsed;
            if (wait > TimeSpan.Zero)
                await Task.Delay(wait); // в Go — time.Sleep в своей горутине
        }

        // PeriodicTimer: пропущенные тики не копятся — если тик длился
        // дольше периода, следующий WaitForNextTickAsync вернётся сразу,
        // но один раз. Это поведение time.Ticker, а не накопителя.
        using var timer = new PeriodicTimer(interval);
        while (world.Tick < 8 && await timer.WaitForNextTickAsync())
            world.Step();

        Console.WriteLine($"тиков: {world.Tick}, игроков: {world.Players.Count}, пропущено: {skipped}");
    }
}
