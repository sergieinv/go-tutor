// Одноразовый сигнал «готово» — TaskCompletionSource.
var log = new string[3];
var done = new TaskCompletionSource();
_ = Task.Run(() =>
{
    log[0] = "worker 0: готово";
    done.SetResult(); // done <- struct{}{}
});
await done.Task; // <-done
Console.WriteLine(log[0]);

// Ожидание нескольких задач — Task.WhenAll (в Go — WaitGroup
// или столько получений, сколько горутин).
await Task.WhenAll(
    Task.Run(() => log[1] = "worker 1: готово"),
    Task.Run(() => log[2] = "worker 2: готово"));
Console.WriteLine(log[1]);
Console.WriteLine(log[2]);

// Пинг-понг на событиях: каждый поток ждёт своего, ставит чужое.
// Потоки ОС заблокированы на всё время ожидания.
var pingTurn = new AutoResetEvent(true);
var pongTurn = new AutoResetEvent(false);
var ball = 0;
var ping = new Thread(() =>
{
    for (var i = 0; i < 3; i++)
    {
        pingTurn.WaitOne();
        Console.WriteLine($"ping {++ball}");
        pongTurn.Set();
    }
});
var pong = new Thread(() =>
{
    for (var i = 0; i < 3; i++)
    {
        pongTurn.WaitOne();
        Console.WriteLine($"pong {++ball}");
        pingTurn.Set();
    }
});
ping.Start();
pong.Start();
ping.Join();
pong.Join();
