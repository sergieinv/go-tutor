Environment.SetEnvironmentVariable("APP_PORT", "9000");

// null — не задана, "" — пустая. В Go Getenv в обоих случаях даёт "",
// различает только LookupEnv.
string? token = Environment.GetEnvironmentVariable("APP_TOKEN");
Console.WriteLine(token is null ? "APP_TOKEN не задана" : "APP_TOKEN задана"); // значение секрета не печатаем

// Значения по умолчанию и разбор типов — как в Go, но разбор бросает исключение
var port = int.Parse(Environment.GetEnvironmentVariable("APP_PORT") ?? "8080");
var timeout = TimeSpan.Parse(Environment.GetEnvironmentVariable("APP_TIMEOUT") ?? "00:00:05");
Console.WriteLine($"port={port} timeout={timeout}");

// Подстановка в стиле Windows (%VAR%); незаданные переменные остаются как есть.
// В Go — os.ExpandEnv("$VAR"), незаданные заменяются пустой строкой.
Console.WriteLine(Environment.ExpandEnvironmentVariables("port=%APP_PORT% host=%APP_HOST%"));

// В ASP.NET Core окружение обычно читают через IConfiguration:
//   builder.Configuration.AddEnvironmentVariables(prefix: "APP_");
//   var opts = builder.Configuration.Get<AppOptions>();
// В Go стандартного аналога нет — загрузчик пишется обычной функцией.
