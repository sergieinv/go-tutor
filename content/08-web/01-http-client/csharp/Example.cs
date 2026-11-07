using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;

// В ASP.NET Core клиент берут из IHttpClientFactory:
// builder.Services.AddHttpClient("users", c => c.Timeout = TimeSpan.FromSeconds(5));
// Здесь — один долгоживущий экземпляр, как переменная client в Go.
var handler = new SocketsHttpHandler { MaxConnectionsPerServer = 100 }; // ≈ http.Transport
using var client = new HttpClient(handler)
{
    Timeout = TimeSpan.FromSeconds(5), // по умолчанию 100 с; в Go у DefaultClient — 0 (без лимита)
};

using var cts = new CancellationTokenSource(TimeSpan.FromMilliseconds(50)); // ≈ context.WithTimeout
using var req = new HttpRequestMessage(HttpMethod.Get, "http://127.0.0.1:8080/user");
req.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("application/json"));

try
{
    // using вместо defer resp.Body.Close(); дочитывать тело не нужно.
    using var resp = await client.SendAsync(req, cts.Token);
    if (resp.StatusCode == HttpStatusCode.NotFound)
        Console.WriteLine("нет пользователя"); // как в Go: статус — часть ответа

    resp.EnsureSuccessStatusCode(); // бросит HttpRequestException; в Go такой проверки нет
    var user = await resp.Content.ReadFromJsonAsync<User>(cts.Token);
    Console.WriteLine(user);
}
catch (TaskCanceledException)
{
    // Таймаут и отмена — исключение; в Go — errors.Is(err, context.DeadlineExceeded)
    Console.WriteLine("дедлайн");
}
catch (HttpRequestException e) when (e.StatusCode is not null)
{
    Console.WriteLine($"статус {(int)e.StatusCode}"); // ≈ errors.As(err, &statusErr)
}
catch (HttpRequestException e)
{
    Console.WriteLine($"сеть: {e.Message}"); // DNS, отказ в соединении — err из client.Do
}

// Лимит размера ответа — на уровне клиента целиком:
// client.MaxResponseContentBufferSize = 4096;

record User(string Name, string Agent);
