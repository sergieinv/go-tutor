var builder = WebApplication.CreateBuilder(args);
// Лимит на остановку: Generic Host ждёт Kestrel и hosted-сервисы
// не дольше ShutdownTimeout. В Go — context.WithTimeout для Shutdown.
builder.Services.Configure<HostOptions>(o => o.ShutdownTimeout = TimeSpan.FromSeconds(10));
var app = builder.Build();

var ready = true; // в Go — atomic.Bool
app.MapGet("/ready", () => ready ? Results.Ok() : Results.StatusCode(503));
app.MapGet("/slow", async () =>
{
    await Task.Delay(TimeSpan.FromSeconds(2)); // дорабатывает при остановке
    return "отчёт готов";
});

// SIGTERM/Ctrl+C хост ловит сам и вызывает ApplicationStopping —
// в Go это отмена контекста из signal.NotifyContext.
var lifetime = app.Services.GetRequiredService<IHostApplicationLifetime>();
lifetime.ApplicationStopping.Register(() =>
{
    ready = false; // /ready отвечает 503 — балансировщик уберёт экземпляр
    // Колбэки ApplicationStopping выполняются до остановки Kestrel, и он пока
    // принимает запросы. Без паузы слушатель закроется сразу и 503 никто не увидит.
    // Пауза идёт в счёт ShutdownTimeout; в проде — несколько секунд.
    Thread.Sleep(TimeSpan.FromSeconds(1));
    Console.WriteLine("остановка: ждём активные запросы");
});
// ApplicationStopped — момент, когда в Go Shutdown вернул nil.
lifetime.ApplicationStopped.Register(() => Console.WriteLine("сервер остановлен"));

await app.RunAsync(); // вернётся после мягкой остановки Kestrel

// Зависимости — после сервера: RunAsync перед возвратом уже освободил контейнер DI
// (синглтоны — в обратном порядке создания). В Go — явный db.Close() после Shutdown.
Console.WriteLine("контейнер DI освобождён");
