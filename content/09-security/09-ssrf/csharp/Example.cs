using System.Net;
using System.Net.Sockets;

// ConnectCallback у SocketsHttpHandler — ближайший аналог Dialer.ControlContext:
// вызывается на каждое новое соединение (и после редиректа тоже). Разница в том,
// что DNS здесь резолвите вы сами: проверяйте и подключайтесь к одному и тому же
// списку адресов, иначе — DNS rebinding.
var handler = new SocketsHttpHandler
{
    UseProxy = false,              // ≈ Transport.Proxy = nil
    MaxAutomaticRedirections = 3,  // ≈ CheckRedirect: len(via) > 3 — ошибка
    ConnectTimeout = TimeSpan.FromSeconds(5),
    ConnectCallback = async (ctx, ct) =>
    {
        IPAddress[] addrs = await Dns.GetHostAddressesAsync(ctx.DnsEndPoint.Host, ct);
        foreach (var ip in addrs)
            if (!IsPublic(ip))
                throw new HttpRequestException($"адрес {ip} запрещён");

        var socket = new Socket(SocketType.Stream, ProtocolType.Tcp) { NoDelay = true };
        try
        {
            await socket.ConnectAsync(addrs, ctx.DnsEndPoint.Port, ct); // те же адреса, что проверили
            return new NetworkStream(socket, ownsSocket: true);
        }
        catch
        {
            socket.Dispose();
            throw;
        }
    },
};
using var client = new HttpClient(handler) { Timeout = TimeSpan.FromSeconds(10) };

Console.WriteLine(IsPublic(IPAddress.Parse("::ffff:127.0.0.1"))); // False
Console.WriteLine(IsPublic(IPAddress.Parse("8.8.8.8")));          // True

// ≈ isPublic в Go. В .NET нет готового IsPrivate/IsGlobalUnicast, как у netip.Addr:
// диапазоны перечисляют сами (IPNetwork — с .NET 8).
static bool IsPublic(IPAddress ip)
{
    if (ip.IsIPv4MappedToIPv6) ip = ip.MapToIPv4(); // ≈ Addr.Unmap()
    string[] blocked =
    [
        "0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
        "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/3",
        "::/127", "fc00::/7", "fe80::/10", "ff00::/8",
    ];
    return !blocked.Any(n => IPNetwork.Parse(n).Contains(ip));
}
