// Тип перед именем; var — вывод типа, как := в Go
int a = 10;
var b = 2.5;

// Локальную переменную без значения нельзя прочитать:
int n;
// Console.WriteLine(n); // CS0165: use of unassigned local variable

// default(T) — аналог нулевого значения, но для string это null
string? s = default; // null, а в Go — ""
Console.WriteLine(s is null); // True

// Обмен через кортежи — как x, y = y, x в Go
var (x, y) = (1, 2);
(x, y) = (y, x);

// Затенение во вложенном блоке C# запрещает (CS0136),
// а Go молча создаёт новую переменную
if (x > 0)
{
    // var x = "строка"; // CS0136
}

// Отбрасывание значения — тот же символ _
_ = int.TryParse("42", out var parsed);
