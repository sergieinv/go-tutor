using System.Net;
using System.Net.Http.Json;

// Один HttpClient на процесс поверх настроенного SocketsHttpHandler —
// аналог одного http.Transport. new HttpClient() на каждый запрос
// приводит к socket exhaustion: сокеты копятся в TIME_WAIT.
var handler = new SocketsHttpHandler
{
    MaxConnectionsPerServer = 64,                           // ≈ MaxConnsPerHost
    PooledConnectionIdleTimeout = TimeSpan.FromSeconds(90), // ≈ IdleConnTimeout
    PooledConnectionLifetime = TimeSpan.FromMinutes(5),     // в Go прямого аналога нет
    ConnectTimeout = TimeSpan.FromSeconds(3),               // ≈ net.Dialer.Timeout
    EnableMultipleHttp2Connections = true,
};
var client = new HttpClient(handler) { Timeout = TimeSpan.FromSeconds(10) }; // ≈ Client.Timeout

Console.WriteLine($"MaxConnectionsPerServer: {handler.MaxConnectionsPerServer}");
Console.WriteLine($"Timeout: {client.Timeout}");

// ResponseHeadersRead — тело остаётся потоком из соединения, как всегда
// в Go; using закрывает ответ и возвращает соединение в пул.
static async Task<T?> GetJsonAsync<T>(HttpClient client, string url, CancellationToken ct)
{
    using var resp = await client.GetAsync(url, HttpCompletionOption.ResponseHeadersRead, ct);
    if (resp.StatusCode != HttpStatusCode.OK)
        throw new HttpRequestException($"unexpected status {(int)resp.StatusCode}", null, resp.StatusCode);
    if (resp.Content.Headers.ContentLength > 1 << 20)
        throw new InvalidOperationException("response body too large");
    return await resp.Content.ReadFromJsonAsync<T>(ct);
}

// Вызов не выполняем: в примере нет сервера, важна конфигурация.
_ = (Func<Task>)(() => GetJsonAsync<Dictionary<string, int>>(client, "http://inventory.internal/stock", CancellationToken.None));
