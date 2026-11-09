// C#: TCP-прокси — два CopyToAsync навстречу друг другу; подпись
// данных игрока — HMACSHA256 и сравнение за постоянное время.
using System.Net;
using System.Net.Sockets;
using System.Security.Cryptography;
using System.Text;

static class Proxy
{
    static readonly byte[] Secret = Encoding.UTF8.GetBytes("общий секрет прокси и бэкендов");

    public static byte[] Sign(string player) =>
        HMACSHA256.HashData(Secret, Encoding.UTF8.GetBytes(player)); // .NET 6+

    // Бэкенд: подпись сравнивается только FixedTimeEquals — аналог hmac.Equal.
    public static bool Verify(string player, byte[] sig) =>
        CryptographicOperations.FixedTimeEquals(sig, Sign(player));

    // Прокси без разбора протокола: байты в обе стороны, пока одна
    // из сторон не закроется. В Go — два io.Copy в горутинах.
    public static async Task PipeAsync(TcpClient client, IPEndPoint backend, CancellationToken ct)
    {
        using var server = new TcpClient();
        await server.ConnectAsync(backend, ct);
        var a = client.GetStream();
        var b = server.GetStream();
        var up = a.CopyToAsync(b, ct);
        var down = b.CopyToAsync(a, ct);
        await Task.WhenAny(up, down);
        // Одна сторона закрылась — закрываем обе, иначе второе
        // копирование висит в чтении, и дожидаемся его.
        client.Close();
        server.Close();
        try { await Task.WhenAll(up, down); }
        catch (Exception e) when (e is IOException or ObjectDisposedException or OperationCanceledException) { }
    }

    // Выбор лобби: наименьшая нагрузка, при равенстве — первое.
    public static string PickLobby(IReadOnlyList<string> lobbies, IReadOnlyDictionary<string, int> online) =>
        lobbies.MinBy(l => online.GetValueOrDefault(l))!;
}

static class Program
{
    static void Main()
    {
        var sig = Proxy.Sign("Steve");
        Console.WriteLine(Proxy.Verify("Steve", sig)); // True
        Console.WriteLine(Proxy.Verify("Notch", sig)); // False
        var online = new Dictionary<string, int> { ["lobby-1"] = 1, ["lobby-2"] = 0 };
        Console.WriteLine(Proxy.PickLobby(["lobby-1", "lobby-2"], online)); // lobby-2
    }
}
