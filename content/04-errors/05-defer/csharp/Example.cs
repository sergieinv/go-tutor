// using закрывает ресурс в конце блока, в обратном порядке объявления
using (var a = new Resource("a"))
using (var b = new Resource("b"))
{
    Console.WriteLine("работаем с a и b");
} // b.Dispose(), затем a.Dispose()

// finally — для произвольного кода «на выходе»
var x = 1;
try
{
    x = 2;
}
finally
{
    Console.WriteLine($"finally видит x = {x}"); // 2: читается в момент выполнения
}

// В цикле using закрывает ресурс на каждой итерации — это блочная область.
// defer в Go привязан к функции, а не к блоку.
foreach (var name in new[] { "x", "y" })
{
    using var r = new Resource(name);
    Console.WriteLine($"обработка {name}");
}

class Resource(string name) : IDisposable
{
    // Dispose не возвращает ошибку: сбой закрытия — только исключение
    public void Dispose() => Console.WriteLine($"close {name}");
}
