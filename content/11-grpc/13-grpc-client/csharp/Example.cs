// Пакеты: Grpc.Net.Client, Microsoft.Extensions.DependencyInjection.
using Grpc.Core;
using Grpc.Net.Client;
using Grpc.Net.Client.Balancer;
using Grpc.Net.Client.Configuration;
using Microsoft.Extensions.DependencyInjection;

// Адреса трёх экземпляров отдаём сами — как manual resolver в Go.
// В проде — "dns:///stock.shop.svc.cluster.local:50051" (headless Service).
var services = new ServiceCollection();
services.AddSingleton<ResolverFactory>(new StaticResolverFactory(_ =>
[
    new BalancerAddress("10.0.0.11", 50051),
    new BalancerAddress("10.0.0.12", 50051),
    new BalancerAddress("10.0.0.13", 50051),
]));

var retry = new MethodConfig
{
    Names = { new MethodName { Service = "stock.v1.StockService" } },
    RetryPolicy = new RetryPolicy
    {
        MaxAttempts = 3, // как и в Go, клиент режет до MaxRetryAttempts (5)
        InitialBackoff = TimeSpan.FromMilliseconds(10),
        MaxBackoff = TimeSpan.FromMilliseconds(100),
        BackoffMultiplier = 2,
        RetryableStatusCodes = { StatusCode.Unavailable },
    },
    // В .NET есть и HedgingPolicy (параллельные копии запроса);
    // в grpc-go hedging не реализован.
};

// Канал — долгоживущий объект на весь сервис, как *grpc.ClientConn.
// В ASP.NET Core его обычно создаёт AddGrpcClient<T>() (Grpc.Net.ClientFactory).
using var channel = GrpcChannel.ForAddress("static:///stock", new GrpcChannelOptions
{
    Credentials = ChannelCredentials.Insecure, // только для примера; в проде — TLS
    ServiceProvider = services.BuildServiceProvider(),
    ServiceConfig = new ServiceConfig
    {
        LoadBalancingConfigs = { new RoundRobinConfig() },
        MethodConfigs = { retry },
    },
    HttpHandler = new SocketsHttpHandler
    {
        // keepalive: как keepalive.ClientParameters{Time, Timeout} в Go.
        KeepAlivePingDelay = TimeSpan.FromSeconds(60),
        KeepAlivePingTimeout = TimeSpan.FromSeconds(20),
        PooledConnectionIdleTimeout = Timeout.InfiniteTimeSpan,
        EnableMultipleHttp2Connections = true,
    },
});

// Подключение тоже ленивое: State == Idle до первого вызова или ConnectAsync.
Console.WriteLine(channel.State);

// WaitForReady на вызов — как grpc.WaitForReady(true):
var options = new CallOptions(deadline: DateTime.UtcNow.AddSeconds(2)).WithWaitForReady();
// var resp = await new StockService.StockServiceClient(channel).GetStockAsync(req, options);
_ = options;
