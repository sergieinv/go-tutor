// Исходящая очередь соединения на .NET: ограниченный Channel<T>,
// TryWrite из тика (не ждёт), отдельная задача-писатель. В Go то же
// самое — буферизованный chan, select с default и горутина-писатель.
using System.Net;
using System.Net.Sockets;
using System.Threading.Channels;

public sealed class Client
{
    private readonly Socket _socket;
    private readonly Channel<byte[]> _queue;
    private readonly CancellationTokenSource _closed = new();
    private int _closing; // 0 — открыт, 1 — закрывается (Interlocked)

    public Exception? Reason { get; private set; }
    public Task Writer { get; }

    public Client(Socket socket, int queue)
    {
        _socket = socket;
        // С FullMode = Wait TryWrite при полной очереди возвращает false,
        // и решение — отключить — принимаем сами. С DropWrite/DropOldest
        // он вернёт true и молча выбросит сообщение — для игры хуже.
        _queue = Channel.CreateBounded<byte[]>(new BoundedChannelOptions(queue)
        {
            SingleReader = true,
            FullMode = BoundedChannelFullMode.Wait,
        });
        Writer = Task.Run(WriteLoopAsync);
    }

    // Вызывается из тика: никогда не ждёт.
    public bool Send(byte[] msg)
    {
        if (_queue.Writer.TryWrite(msg)) return true;
        Close(new IOException("исходящая очередь переполнена"));
        return false;
    }

    public void Close(Exception reason)
    {
        if (Interlocked.Exchange(ref _closing, 1) != 0) return; // только первый
        Reason = reason;
        _queue.Writer.TryComplete(); // в .NET канал «закрывают» — TryWrite после этого false
        _closed.Cancel();            // будит писателя, ждущего в SendAsync
        _socket.Close();
    }

    private async Task WriteLoopAsync()
    {
        try
        {
            await foreach (var msg in _queue.Reader.ReadAllAsync(_closed.Token))
            {
                // Socket.SendTimeout действует только на синхронный Send;
                // для async — токен с таймаутом (в Go — SetWriteDeadline).
                using var timeout = CancellationTokenSource.CreateLinkedTokenSource(_closed.Token);
                timeout.CancelAfter(TimeSpan.FromSeconds(5));
                await _socket.SendAsync(msg, SocketFlags.None, timeout.Token);
            }
        }
        catch (Exception e) when (e is OperationCanceledException or SocketException or ObjectDisposedException)
        {
            Close(e);
        }
    }
}

public static class Program
{
    public static async Task Main()
    {
        using var listener = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp);
        listener.Bind(new IPEndPoint(IPAddress.Loopback, 0));
        listener.Listen();

        using var bot = new Socket(AddressFamily.InterNetwork, SocketType.Stream, ProtocolType.Tcp);
        await bot.ConnectAsync(listener.LocalEndPoint!);
        var server = await listener.AcceptAsync();

        // Бот не читает: буферы ядра заполнятся, писатель встанет,
        // очередь переполнится — клиент отключён, тик не ждал.
        var client = new Client(server, queue: 8);
        var update = new byte[2048];
        var ticks = 0;
        while (client.Send(update) && ticks < 100_000) ticks++;
        await client.Writer;
        Console.WriteLine($"отключён: {client.Reason?.Message}");
    }
}
