// dotnet new console && dotnet run   ↔   go mod init && go run .
// dotnet test                        ↔   go test ./...
// dotnet format                      ↔   gofmt -w .  (без настроек)
// Roslyn-анализаторы                 ↔   go vet ./...

// Интерполяцию проверяет компилятор: несовпадение
// «верба» и аргумента в C# просто невозможно
var count = 5;
Console.WriteLine($"{count} товаров");

// А вот составной формат — та же ловушка, что в Go:
// компилируется, но падает во время выполнения
try
{
    Console.WriteLine(string.Format("{0} и {1}", "чай")); // FormatException
}
catch (FormatException e)
{
    Console.WriteLine(e.Message);
}

// Отличие: в Go fmt не бросает исключение, а вставляет
// в вывод метку вроде %!s(MISSING) — поэтому без go vet
// такая ошибка легко доезжает до продакшена.
