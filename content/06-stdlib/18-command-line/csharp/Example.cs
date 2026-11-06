// System.CommandLine 2.0 (NuGet, стабильная версия с ноября 2025):
// дерево команд, типизированные опции, справка и автодополнение.
using System.CommandLine;

var port = new Option<int>("--port") { Description = "порт HTTP-сервера", DefaultValueFactory = _ => 8080 };
var host = new Option<string>("--host") { Description = "адрес", DefaultValueFactory = _ => "localhost" };
var verbose = new Option<bool>("--verbose", "-v") { Description = "подробный лог" };
var tags = new Option<string[]>("--tag") { Description = "метка (можно повторять)" };
var files = new Argument<string[]>("files") { DefaultValueFactory = _ => [] };

var serve = new Command("serve", "запустить сервер") { port, host, verbose, tags, files };
// Значения достают из ParseResult — как разыменование указателей после fs.Parse
serve.SetAction(r => Console.WriteLine(
    $"serve {r.GetValue(host)}:{r.GetValue(port)} v={r.GetValue(verbose)} " +
    $"tags=[{string.Join(" ", r.GetValue(tags) ?? [])}] files=[{string.Join(" ", r.GetValue(files) ?? [])}]"));

var steps = new Option<int>("--steps") { Description = "сколько миграций применить", DefaultValueFactory = _ => 1 };
var migrate = new Command("migrate") { steps };
migrate.SetAction(r => Console.WriteLine($"migrate steps = {r.GetValue(steps)}"));

var root = new RootCommand("app") { serve, migrate };

// args в Main не содержит имени программы — в Go это os.Args[1:].
// Здесь, как и в примере на Go, разбираем заданный массив.
root.Parse(["serve", "--port", "9000", "-v", "--tag", "api", "a.txt"]).Invoke();
root.Parse(["migrate", "--steps", "3"]).Invoke();
// --help печатает справку, собранную из описаний, — как -h и PrintDefaults в flag
