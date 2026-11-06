// Таймаут — CancellationTokenSource с CancelAfter (WithTimeout в Go)
using var timeout = new CancellationTokenSource(TimeSpan.FromMilliseconds(50));
try
{
    await SlowQuery(TimeSpan.FromSeconds(1), timeout.Token);
}
catch (OperationCanceledException)
{
    // В Go это обычная ошибка: errors.Is(err, context.DeadlineExceeded)
    Console.WriteLine("медленный запрос: отменён");
}

// Дерево: связанный источник отменяется вместе с родителем (WithCancel(parent))
using var parent = new CancellationTokenSource();
using var child = CancellationTokenSource.CreateLinkedTokenSource(parent.Token);
child.Token.Register(() => Console.WriteLine("child отменён")); // context.AfterFunc
parent.Cancel();
Console.WriteLine($"child: {child.IsCancellationRequested}"); // True

// Значения запроса в .NET обычно живут не в токене, а в AsyncLocal<T>
// или HttpContext.Items. В Go для этого есть context.WithValue.
var requestId = new AsyncLocal<string?> { Value = "req-42" };
Console.WriteLine($"request id: {requestId.Value}");

static async Task<string> SlowQuery(TimeSpan d, CancellationToken ct)
{
    await Task.Delay(d, ct); // бросит OperationCanceledException при отмене
    return "результат";
}
