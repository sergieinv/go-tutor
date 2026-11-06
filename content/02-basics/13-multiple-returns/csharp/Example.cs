// out-параметр — в Go второй результат: n, err := strconv.Atoi(s)
if (int.TryParse("42", out var n)) Console.WriteLine(n);

// TryGetValue — в Go age, ok := ages["bob"]
var ages = new Dictionary<string, int> { ["ann"] = 30 };
if (!ages.TryGetValue("bob", out var age)) Console.WriteLine($"bob: нет, age = {age}");

// Кортеж — один объект, его можно сохранить целиком.
// В Go несколько результатов нельзя положить в одну переменную.
var res = DivMod(17, 5);
Console.WriteLine(res.q + res.r);
var (q, r) = DivMod(17, 5);
Console.WriteLine($"{q} {r}");

// Ошибка — исключение, а не результат
try { SafeDiv(1, 0); }
catch (DivideByZeroException e) { Console.WriteLine(e.Message); }

static (int q, int r) DivMod(int a, int b) => (a / b, a % b);
static int SafeDiv(int a, int b) =>
    b == 0 ? throw new DivideByZeroException("деление на ноль") : a / b;
