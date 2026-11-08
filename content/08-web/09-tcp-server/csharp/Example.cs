using System.Buffers.Binary;
using System.Net;
using System.Net.Sockets;

// net.Listen("tcp", "127.0.0.1:0") — TcpListener на порту 0.
var listener = new TcpListener(IPAddress.Loopback, 0);
listener.Start();
using var cts = new CancellationTokenSource();
var acceptLoop = AcceptLoop(listener, cts.Token);

var port = ((IPEndPoint)listener.LocalEndpoint).Port;
using (var client = new TcpClient())
{
    await client.ConnectAsync(IPAddress.Loopback, port);
    var stream = client.GetStream();
    var writer = new StreamWriter(stream) { AutoFlush = true, NewLine = "\n" };
    var reader = new StreamReader(stream); // один reader на соединение, как bufio.Reader
    await writer.WriteLineAsync("PING");
    Console.WriteLine(await reader.ReadLineAsync()); // PONG
}

cts.Cancel();     // в Go — ln.Close(), Accept вернёт net.ErrClosed
listener.Stop();
await acceptLoop;

static async Task AcceptLoop(TcpListener listener, CancellationToken ct)
{
    var handlers = new List<Task>(); // в Go — sync.WaitGroup
    using var sem = new SemaphoreSlim(100); // лимит соединений — семафор, как канал sem в Go
    try
    {
        while (true)
        {
            await sem.WaitAsync(ct);
            var client = await listener.AcceptTcpClientAsync(ct);
            handlers.RemoveAll(t => t.IsCompleted); // список не растёт бесконечно
            handlers.Add(Task.Run(async () => // go handle(conn)
            {
                try { await Handle(client, ct); }
                finally { sem.Release(); }
            }));
        }
    }
    catch (OperationCanceledException) { }
    await Task.WhenAll(handlers);
}

static async Task Handle(TcpClient client, CancellationToken ct)
{
    using var _ = client; // defer conn.Close()
    var stream = client.GetStream();
    using var reader = new StreamReader(stream);
    var writer = new StreamWriter(stream) { AutoFlush = true, NewLine = "\n" };
    using var idle = CancellationTokenSource.CreateLinkedTokenSource(ct);
    try
    {
        while (true)
        {
            // Таймаут простоя продлеваем перед каждым чтением — в Go SetReadDeadline.
            idle.CancelAfter(TimeSpan.FromMilliseconds(200));
            var line = await ReadLineLimited(reader, 1024, idle.Token);
            if (line is null) break; // EOF или слишком длинная строка
            await writer.WriteLineAsync(line == "PING" ? "PONG" : "ERR unknown command");
        }
    }
    catch (OperationCanceledException) { } // простой или остановка — в Go os.ErrDeadlineExceeded
}

// ReadLineAsync длину строки не ограничивает: клиент без '\n' займёт всю память.
// Лимит, как sc.Buffer(..., 1024) у bufio.Scanner в Go.
static async Task<string?> ReadLineLimited(StreamReader reader, int max, CancellationToken ct)
{
    var sb = new System.Text.StringBuilder();
    var ch = new char[1];
    while (await reader.ReadAsync(ch, ct) == 1)
    {
        if (ch[0] == '\n') return sb.ToString();
        if (sb.Length == max) return null;
        sb.Append(ch[0]);
    }
    return sb.Length > 0 ? sb.ToString() : null;
}

// Префикс длины: BinaryPrimitives вместо encoding/binary,
// ReadExactlyAsync (.NET 7+) — аналог io.ReadFull.
static class Framing
{
    public static async Task<byte[]> ReadFrame(Stream s, int max)
    {
        var hdr = new byte[4];
        await s.ReadExactlyAsync(hdr);
        var n = BinaryPrimitives.ReadUInt32BigEndian(hdr);
        if (n > max) throw new InvalidDataException("frame too large");
        var payload = new byte[n];
        await s.ReadExactlyAsync(payload);
        return payload;
    }
}
