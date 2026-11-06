using System.Collections.Concurrent;

// Гонка: count++ — чтение, сложение, запись. Детектора гонок в .NET нет,
// ошибка проявится как «иногда меньше 100».
var count = 0;
Parallel.For(0, 100, _ => count++);

// lock — аналог sync.Mutex: выход из lock happens-before следующего входа.
var gate = new object();
var safe = 0;
Parallel.For(0, 100, _ => { lock (gate) safe++; });
Console.WriteLine($"lock: {safe}");

// Interlocked — аналог sync/atomic.
var hits = 0;
Parallel.For(0, 100, _ => Interlocked.Increment(ref hits));
Console.WriteLine($"Interlocked: {hits}");

// Dictionary из нескольких потоков может молча испортиться;
// Go map в такой ситуации аварийно завершает программу.
// Потокобезопасная замена — ConcurrentDictionary (в Go — map + Mutex или sync.Map).
var words = new ConcurrentDictionary<string, int>();
Parallel.ForEach(new[] { "a", "b", "a" }, w => words.AddOrUpdate(w, 1, (_, n) => n + 1));
Console.WriteLine($"a = {words["a"]}");

// Lazy<T> — аналог sync.Once для ленивой инициализации.
var config = new Lazy<string>(() => "lazy");
Console.WriteLine(config.Value);
