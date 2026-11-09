// В ASP.NET Core проброс дедлайна и отмены в исходящие gRPC-вызовы включают
// явно (пакет Grpc.AspNetCore.Server.ClientFactory):
//
//   builder.Services.AddGrpcClient<Warehouse.WarehouseClient>(o => o.Address = new("https://warehouse"))
//       .EnableCallContextPropagation(); // берёт Deadline и CancellationToken входящего вызова
//
// В обработчике дедлайн и отмена — разные сущности:
//
//   public override async Task<CreateOrderReply> CreateOrder(CreateOrderRequest r, ServerCallContext ctx)
//   {
//       var left = ctx.Deadline - DateTime.UtcNow;
//       if (left < TimeSpan.FromMilliseconds(150)) throw new RpcException(new(StatusCode.DeadlineExceeded, "не хватит времени"));
//       await warehouse.ReserveAsync(r, deadline: ctx.Deadline - Reserve, cancellationToken: ctx.CancellationToken);
//   }
//
// Ниже — тот же расчёт бюджета без NuGet: TimeProvider вместо Now,
// связанный CancellationTokenSource вместо context.WithDeadline.

public sealed class NotEnoughTimeException() : Exception("not enough time for the call");

public sealed record Budget(TimeSpan Reserve, TimeSpan Max, TimeSpan Min, TimeProvider Time)
{
    // Таймаут исходящего вызова; deadline == null — у входящего запроса дедлайна нет.
    public TimeSpan Timeout(DateTimeOffset? deadline, CancellationToken parent)
    {
        parent.ThrowIfCancellationRequested();
        var now = Time.GetUtcNow();
        var timeout = deadline is { } dl ? Min2(dl - now - Reserve, Max) : Max;
        if (timeout < Min) throw new NotEnoughTimeException();
        return timeout;
    }

    static TimeSpan Min2(TimeSpan a, TimeSpan b) => a < b ? a : b;
}

public static class Program
{
    public static async Task Main()
    {
        var budget = new Budget(TimeSpan.FromMilliseconds(100), TimeSpan.FromSeconds(2), TimeSpan.FromMilliseconds(200), TimeProvider.System);
        var deadline = DateTimeOffset.UtcNow.AddSeconds(1);
        var parent = CancellationToken.None; // в обработчике — ctx.CancellationToken
        var timeout = budget.Timeout(deadline, parent);
        // Таймер — по часам budget.Time (конструктор с TimeProvider — .NET 8+),
        // связанный источник — чтобы дошла и отмена родителя.
        using var timeoutCts = new CancellationTokenSource(timeout, budget.Time);
        using var cts = CancellationTokenSource.CreateLinkedTokenSource(parent, timeoutCts.Token);

        // Аналог Gather: первая ошибка отменяет остальных, ждём всех.
        var calls = new[] { "warehouse", "pricing" }.Select(async name =>
        {
            try { await Task.Delay(50, cts.Token); return $"{name}: ok"; }
            catch { cts.Cancel(); throw; }
        });
        foreach (var r in await Task.WhenAll(calls)) Console.WriteLine(r);

        // Фоновая работа после ответа: аналог context.WithoutCancel + свой таймаут.
        using var bg = new CancellationTokenSource(TimeSpan.FromSeconds(5));
        await Task.Run(() => Console.WriteLine("аудит отправлен"), bg.Token);
    }
}
