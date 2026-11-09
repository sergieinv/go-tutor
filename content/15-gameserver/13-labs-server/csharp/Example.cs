// Рассылка тика на .NET: событие кодируется один раз на протокол в
// ArrayBufferWriter<byte>, готовые байты копируются в пачки получателей,
// пачка уходит в ограниченный Channel<T> через TryWrite — тик не ждёт.
// В Go то же самое — интерфейс Protocol, append в []byte, chan и
// select с default.
using System.Buffers;
using System.Buffers.Binary;
using System.Text;
using System.Threading.Channels;

public readonly record struct GameEvent(string Kind, int Subject, double DX, double DZ);

public interface IProtocol
{
    void Append(IBufferWriter<byte> dst, in GameEvent ev);
}

// «Minecraft»: дельта в 1/4096 блока, big-endian, как BinaryPrimitives.
public sealed class McProtocol : IProtocol
{
    public void Append(IBufferWriter<byte> dst, in GameEvent ev)
    {
        Span<byte> b = dst.GetSpan(5);
        BinaryPrimitives.WriteInt16BigEndian(b, checked((short)Math.Round(ev.DX * 4096)));
        BinaryPrimitives.WriteInt16BigEndian(b[2..], checked((short)Math.Round(ev.DZ * 4096)));
        b[4] = (byte)ev.Subject;
        dst.Advance(5);
    }
}

// «Браузер»: JSON. В .NET для этого есть Utf8JsonWriter без рефлексии.
public sealed class WebProtocol : IProtocol
{
    public void Append(IBufferWriter<byte> dst, in GameEvent ev) =>
        dst.Write(Encoding.UTF8.GetBytes($"{{\"t\":\"{ev.Kind}\",\"id\":{ev.Subject}}},"));
}

public sealed class Client(int id, IProtocol proto, int queue)
{
    public int Id { get; } = id;
    public IProtocol Proto { get; } = proto;
    public HashSet<int> Visible { get; } = new();
    public ArrayBufferWriter<byte> Batch { get; } = new();
    public string? Reason { get; set; }

    // FullMode = Wait: при полной очереди TryWrite вернёт false, и решение
    // (отключить) принимает мир. DropOldest молча сломал бы картину клиента.
    public Channel<byte[]> Out { get; } = Channel.CreateBounded<byte[]>(
        new BoundedChannelOptions(queue) { SingleReader = true, FullMode = BoundedChannelFullMode.Wait });
}

public sealed class World
{
    private readonly Dictionary<int, Client> _clients = new();
    // Кеш «протокол → байты текущего события»: ReferenceEquals, как == у
    // указателей в Go.
    private readonly Dictionary<IProtocol, ArrayBufferWriter<byte>> _cache =
        new(ReferenceEqualityComparer.Instance);
    public int Encodes { get; private set; }

    public void Add(Client c) => _clients[c.Id] = c;

    public void BroadcastToViewers(in GameEvent ev)
    {
        foreach (var w in _cache.Values) w.ResetWrittenCount(); // .NET 8+
        if (!_clients.TryGetValue(ev.Subject, out var src)) return;
        foreach (var id in src.Visible)
        {
            var q = _clients[id];
            if (!_cache.TryGetValue(q.Proto, out var enc))
                _cache[q.Proto] = enc = new ArrayBufferWriter<byte>();
            if (enc.WrittenCount == 0)
            {
                q.Proto.Append(enc, ev);
                Encodes++;
            }
            q.Batch.Write(enc.WrittenSpan);
        }
    }

    // Конец тика: одна пачка на клиента, неблокирующая отправка.
    public List<Client> Flush()
    {
        var slow = new List<Client>();
        foreach (var c in _clients.Values)
        {
            if (c.Batch.WrittenCount == 0) continue;
            byte[] data = c.Batch.WrittenSpan.ToArray(); // в проде — ArrayPool<byte>.Shared
            c.Batch.ResetWrittenCount();
            if (!c.Out.Writer.TryWrite(data))
            {
                c.Reason = "slow_consumer";
                slow.Add(c); // убрать из мира в начале следующего тика
            }
        }
        return slow;
    }
}

public static class Program
{
    public static void Main()
    {
        var mc = new McProtocol();
        var web = new WebProtocol();
        var world = new World();
        var alice = new Client(1, mc, 64);
        var clients = new[] { alice, new Client(2, mc, 64), new Client(3, mc, 64), new Client(4, web, 64) };
        foreach (var c in clients) world.Add(c);
        foreach (var c in clients[1..]) { alice.Visible.Add(c.Id); c.Visible.Add(alice.Id); }

        world.BroadcastToViewers(new GameEvent("move", 1, 2.0, 0.0));
        var slow = world.Flush();
        Console.WriteLine($"кодирований: {world.Encodes}, медленных: {slow.Count}"); // 2 и 0
    }
}
