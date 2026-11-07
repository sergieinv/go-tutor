// В C# зависимость — интерфейс рядом с реализацией и DI-контейнер,
// а в тестах — Moq/NSubstitute (NuGet) или подмена HttpMessageHandler.
// Ниже — то же без пакетов: ручной стаб, фейковые часы через TimeProvider
// и HttpMessageHandler вместо httptest.NewServer.
using System.Net;
using System.Text.Json;

var handler = new StubHandler(req => req.RequestUri!.AbsolutePath == "/rates/USD"
    ? new HttpResponseMessage(HttpStatusCode.OK) { Content = new StringContent("""{"rate": 92.5}""") }
    : new HttpResponseMessage(HttpStatusCode.NotFound));
var source = new HttpRateSource(new HttpClient(handler) { BaseAddress = new Uri("http://rates.test") });
Console.WriteLine(await source.RateAsync("USD")); // 92.5

var stub = new StubSource(90m); // Moq: new Mock<IRateSource>().Setup(...).ReturnsAsync(90m)
var clock = new ManualClock(new DateTimeOffset(2025, 1, 1, 12, 0, 0, TimeSpan.Zero));
var conv = new Converter(stub, clock);
await conv.ConvertAsync(1, "USD");
clock.Now += TimeSpan.FromSeconds(59);
await conv.ConvertAsync(1, "USD");
Console.WriteLine(stub.Calls); // 1 — второй раз из кеша

public interface IRateSource { Task<decimal> RateAsync(string cur); }

public sealed class HttpRateSource(HttpClient http) : IRateSource
{
    public async Task<decimal> RateAsync(string cur)
    {
        using var resp = await http.GetAsync($"/rates/{Uri.EscapeDataString(cur)}");
        resp.EnsureSuccessStatusCode();
        using var doc = JsonDocument.Parse(await resp.Content.ReadAsStringAsync());
        return doc.RootElement.GetProperty("rate").GetDecimal();
    }
}

// TimeProvider (.NET 8) ≈ now func() time.Time; в тестах — FakeTimeProvider из NuGet.
public sealed class ManualClock(DateTimeOffset now) : TimeProvider
{
    public DateTimeOffset Now { get; set; } = now;
    public override DateTimeOffset GetUtcNow() => Now;
}

public sealed class Converter(IRateSource src, TimeProvider clock)
{
    private readonly Dictionary<string, (decimal Rate, DateTimeOffset At)> _cache = new();

    public async Task<decimal> ConvertAsync(decimal amount, string cur)
    {
        if (!_cache.TryGetValue(cur, out var e) || clock.GetUtcNow() - e.At >= TimeSpan.FromMinutes(1))
            _cache[cur] = e = (await src.RateAsync(cur), clock.GetUtcNow());
        return amount * e.Rate;
    }
}

// Стаб + шпион — то, что в Go пишут структурой в _test.go.
public sealed class StubSource(decimal rate) : IRateSource
{
    public int Calls { get; private set; }
    public Task<decimal> RateAsync(string cur) { Calls++; return Task.FromResult(rate); }
}

// Подмена транспорта вместо настоящего сервера (в Go — httptest.NewServer).
public sealed class StubHandler(Func<HttpRequestMessage, HttpResponseMessage> respond) : HttpMessageHandler
{
    protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken ct) =>
        Task.FromResult(respond(request));
}
