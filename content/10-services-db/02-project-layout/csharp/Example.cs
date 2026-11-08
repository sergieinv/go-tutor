using System.ComponentModel.DataAnnotations;
using Microsoft.Extensions.Options;

// Generic Host собирает всё сам: конфигурацию из appsettings.json, переменных
// окружения (ORDERS__MAXCONNS → Orders:MaxConns) и аргументов командной строки,
// контейнер зависимостей, логирование и graceful shutdown по SIGTERM.
var builder = WebApplication.CreateBuilder(args);

// IOptions<T> + валидация атрибутами. ValidateOnStart — аналог fail fast:
// без него ошибка всплывёт при первом обращении к Options.Value.
builder.Services.AddOptions<OrdersOptions>()
    .Bind(builder.Configuration.GetSection("Orders"))
    .ValidateDataAnnotations()
    .ValidateOnStart();

// Регистрация в контейнере: интерфейс → реализация. Порядок создания объектов
// решает контейнер во время выполнения. В Go — явные вызовы в run().
builder.Services.AddSingleton<IOrderStore, InMemoryOrderStore>();
builder.Services.AddSingleton<OrderService>();

var app = builder.Build();
app.MapGet("/stats", (OrderService svc) => svc.Stats());
// app.Run() здесь не вызываем, чтобы пример завершался; в сервисе — app.Run().

public class OrdersOptions
{
    [Required] public string Dsn { get; set; } = "";
    [Range(1, 100)] public int MaxConns { get; set; } = 10;
    public TimeSpan ReadTimeout { get; set; } = TimeSpan.FromSeconds(5);

    // Секрет в логах — забота автора, как и в Go: переопределите ToString
    // или не логируйте объект целиком.
    public override string ToString() => $"MaxConns={MaxConns} ReadTimeout={ReadTimeout} Dsn=***";
}

// Проект по слоям — Models/, Services/, Repositories/ — привычен для .NET.
// В Go пакеты режут по смыслу: orders, stock, а слои — внутри пакета.
public interface IOrderStore
{
    int Count();
}

public class InMemoryOrderStore : IOrderStore
{
    public int Count() => 0;
}

public class OrderService(IOrderStore store, IOptions<OrdersOptions> options)
{
    public string Stats() => $"заказов: {store.Count()}, пул до {options.Value.MaxConns}";
}
