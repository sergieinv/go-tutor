using System.Diagnostics;

// Аналог GOMAXPROCS — число процессоров, которое видит CLR
// (в контейнере учитывает лимит CPU, переопределяется DOTNET_PROCESSOR_COUNT).
Console.WriteLine($"ProcessorCount: {Environment.ProcessorCount}");

// Блокирующее ожидание держит поток пула. До MinThreads (по умолчанию
// ProcessorCount) пул создаёт потоки сразу, дальше — только когда замечает
// голодание (thread pool starvation), и с задержкой. Задач заметно больше
// MinThreads — и потоков становится больше этого порога.
ThreadPool.GetMinThreads(out var minWorkers, out _);
var blocking = Enumerable.Range(0, minWorkers + 20).Select(_ => Task.Run(() => Thread.Sleep(2000))).ToArray();
await Task.Delay(1500);
Console.WriteLine($"потоков пула больше MinThreads: {ThreadPool.ThreadCount > minWorkers}");
await Task.WhenAll(blocking);

// async-ожидание поток не держит: машина состояний, которую сгенерировал
// компилятор, освобождает поток до завершения таймера или I/O.
// В Go то же даёт netpoller — без async/await в коде.
var sw = Stopwatch.StartNew();
await Task.WhenAll(Enumerable.Range(0, 1000).Select(async _ => await Task.Delay(100)));
Console.WriteLine($"1000 ожиданий заняли ~100 мс: {sw.ElapsedMilliseconds < 1000}");

// Долгий цикл в задаче занимает поток пула целиком: CLR не вытесняет
// рабочий элемент, это делает только ОС — вместе с потоком.
var spin = Task.Run(() => { var x = 0L; for (var i = 0; i < 500_000_000; i++) x += i; return x; });
Console.WriteLine($"сумма: {await spin}");

// Аналог runtime.Gosched — уступить очередь другим работам пула.
await Task.Yield();
