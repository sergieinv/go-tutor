// Lazy<T> — аналог sync.OnceValue: фабрика вызывается один раз,
// остальные потоки ждут результата (режим ExecutionAndPublication)
var loads = 0;
var config = new Lazy<Config>(() =>
{
    Interlocked.Increment(ref loads);
    return new Config("postgres://localhost/app");
});
Parallel.For(0, 10, i => _ = config.Value);
Console.WriteLine($"{config.Value.Dsn} — загрузок: {loads}");

// Исключение фабрики кешируется: каждый .Value бросает его снова —
// как паника в OnceValue/OnceFunc. Повтора не будет.
var token = new Lazy<string>(() => throw new FileNotFoundException("нет файла с токеном"));
for (var i = 0; i < 2; i++)
{
    try { _ = token.Value; }
    catch (FileNotFoundException e) { Console.WriteLine($"ошибка: {e.Message}"); }
}

// PublicationOnly не кеширует исключения — следующий .Value попробует снова.
// В Go готового аналога нет: Once не даёт второй попытки.
var retrying = new Lazy<string>(() => DateTime.Now.Ticks.ToString(), LazyThreadSafetyMode.PublicationOnly);
Console.WriteLine(retrying.IsValueCreated); // False — до первого обращения

record Config(string Dsn);
