namespace HelloWorld;

// Классический вариант: точка входа — статический метод класса
internal static class Program
{
    private static string Greet(string name) => $"Привет, {name}!";

    private static int Main(string[] args)
    {
        Console.WriteLine("Hello, World");
        Console.WriteLine(Greet("Go"));

        // Интерполяция вместо Printf/Sprintf
        var name = "Аня";
        var day = 3;
        Console.WriteLine($"{name} изучает Go {day}-й день");
        var line = $"{2} + {2} = {2 + 2}";
        Console.WriteLine(line);

        Console.WriteLine("громко".ToUpper());
        Console.WriteLine(new string('=', 10));

        // Код возврата — результат Main; в Go — os.Exit(0)
        return 0;
    }
}

// Top-level statements (C# 9+) выглядят как Go, но компилятор
// всё равно генерирует класс Program с методом Main.
