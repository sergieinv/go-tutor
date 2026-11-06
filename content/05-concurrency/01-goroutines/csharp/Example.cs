// Task.Run ставит делегат в пул потоков и возвращает Task:
// через него ждут завершения и получают результат.
var tasks = Enumerable.Range(0, 5).Select(i => Task.Run(() => i * i)).ToArray();
int[] squares = await Task.WhenAll(tasks);
Console.WriteLine($"квадраты: [{string.Join(" ", squares)}]");

// Асинхронность «заражает» сигнатуры: кто ждёт async-метод,
// сам становится async. В Go функция остаётся синхронной,
// а конкурентность выбирает вызывающий оператором go.
static async Task<int> LoadAsync(int id)
{
    await Task.Delay(10); // не блокирует поток
    return id * 10;
}
Console.WriteLine(await LoadAsync(4));

// Отдельный поток ОС — дорогой ресурс, их не создают тысячами.
var thread = new Thread(() => Console.WriteLine("из потока"));
thread.Start();
thread.Join(); // аналог ожидания горутины через WaitGroup

// Сколько потоков пул держит активными — настраивается отдельно;
// ближайший аналог GOMAXPROCS — число ядер:
Console.WriteLine($"ядер: {Environment.ProcessorCount}");
