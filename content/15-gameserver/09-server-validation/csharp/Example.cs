using System;
using System.Text;

// Те же проверки на C#. IEEE 754 одинаков: NaN не равен ничему, и любое
// сравнение с ним ложно, поэтому `d2 > max2` пропускает NaN и в C#.
var s = new Session(x: 0, z: 0, tick: 0);
Console.WriteLine(s.Move(1, 0.5, 0));        // Accept
Console.WriteLine(s.Move(2, 9, 0));          // Reject
Console.WriteLine(s.Move(3, double.NaN, 0)); // Kick
Console.WriteLine(Session.ChatOk("привет\u0007")); // False: управляющий символ

double nan = double.NaN;
Console.WriteLine(nan * nan > 1.0); // False: наивная проверка NaN не видит

enum Verdict { Accept, Reject, Kick }

sealed class Session(double x, double z, ulong tick)
{
    private const double MaxCoord = 3e7, MaxSpeed = 1.0;
    private const ulong MaxCatchUp = 5;
    private double _x = x, _z = z;
    private ulong _at = tick;
    // Ведро на тиках, а не TokenBucketRateLimiter: тот пополняется по
    // реальному времени (ReplenishmentPeriod); в тестах — AutoReplenishment = false
    // и ручной TryReplenish().
    private readonly TickBucket _chat = new(burst: 3, every: 20);

    public Verdict Move(ulong tick, double x, double z)
    {
        // double.IsFinite — то же, что !IsNaN && !IsInfinity.
        if (!double.IsFinite(x) || !double.IsFinite(z) || Math.Abs(x) > MaxCoord || Math.Abs(z) > MaxCoord)
            return Verdict.Kick;
        double budget = MaxSpeed * Math.Clamp(tick - _at, 1UL, MaxCatchUp);
        double dx = x - _x, dz = z - _z;
        if (!(dx * dx + dz * dz <= budget * budget)) return Verdict.Reject;
        (_x, _z, _at) = (x, z, tick);
        return Verdict.Accept;
    }

    public bool Say(ulong tick, string msg) => ChatOk(msg) && _chat.Take(tick);

    // Длина — в кодовых точках (Rune), а не в char: string в .NET — UTF-16,
    // и эмодзи занимает два char. В Go string — байты UTF-8,
    // и символы считает utf8.RuneCountInString.
    public static bool ChatOk(string msg)
    {
        int n = 0;
        foreach (Rune r in msg.EnumerateRunes())
        {
            if (Rune.IsControl(r) || r.Value == '§') return false;
            if (++n > 256) return false;
        }
        return true;
    }
}

sealed class TickBucket(int burst, int every)
{
    private readonly int _burst = burst;
    private int _tokens = burst;
    private ulong _last;

    public bool Take(ulong tick)
    {
        int n = (int)((tick - _last) / (ulong)every);
        if (n > 0) { _tokens = Math.Min(_burst, _tokens + n); _last += (ulong)(n * every); }
        if (_tokens == 0) return false;
        _tokens--;
        return true;
    }
}
