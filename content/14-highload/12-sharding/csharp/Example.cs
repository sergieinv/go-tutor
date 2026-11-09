using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;

// string.GetHashCode() в .NET Core рандомизирован на каждый процесс:
// для шардирования он не годится, нужен стабильный хеш (здесь FNV-1a).
static ulong Hash64(string s)
{
    ulong h = 14695981039346656037;
    foreach (byte b in System.Text.Encoding.UTF8.GetBytes(s))
    {
        h ^= b;
        h *= 1099511628211;
    }
    // Финализатор fmix64 — перемешать биты, как в примере на Go.
    h ^= h >> 33; h *= 0xff51afd7ed558ccd;
    h ^= h >> 33; h *= 0xc4ceb9fe1a85ec53;
    h ^= h >> 33;
    return h;
}

var ring = new Ring(Hash64, vnodes: 160);
foreach (var s in new[] { "orders-1", "orders-2", "orders-3" }) ring.Add(s);
Console.WriteLine(ring.Get("customer-42"));

// Scatter-gather: Task.WhenAll вместо errgroup, отмена — CancellationToken.
var shards = new Dictionary<string, Func<int, CancellationToken, Task<Order[]>>>
{
    ["orders-1"] = (limit, ct) => Task.FromResult(new[] { new Order(1, DateTime.UnixEpoch.AddMinutes(5)) }),
    ["orders-2"] = (limit, ct) => Task.FromResult(new[] { new Order(2, DateTime.UnixEpoch.AddMinutes(9)) }),
};
using var cts = new CancellationTokenSource();
var parts = await Task.WhenAll(shards.Values.Select(q => q(10, cts.Token)));
var recent = parts.SelectMany(p => p)
    .OrderByDescending(o => o.CreatedAt).ThenBy(o => o.Id)
    .Take(10);
Console.WriteLine(string.Join(",", recent.Select(o => o.Id)));

record Order(long Id, DateTime CreatedAt);

sealed class Ring(Func<string, ulong> hash, int vnodes)
{
    // Отсортированный массив точек и владельцы — как срез и map в Go.
    private ulong[] _points = [];
    private readonly Dictionary<ulong, string> _owner = new();

    public void Add(string shard)
    {
        for (int i = 0; i < vnodes; i++) _owner[hash($"{shard}#{i}")] = shard;
        _points = _owner.Keys.Order().ToArray();
    }

    public string Get(string key)
    {
        int i = Array.BinarySearch(_points, hash(key));
        if (i < 0) i = ~i;                  // первая точка больше ключа
        if (i == _points.Length) i = 0;     // по кругу
        return _owner[_points[i]];
    }
}
