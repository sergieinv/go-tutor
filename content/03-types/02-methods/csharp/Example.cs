// Методы объявляются внутри типа, this — неявный
var c = new Counter("home"); // настоящий конструктор
c.Hit();
Console.WriteLine(c); // HOME=1

// Для struct C# тоже различает копию и ссылку:
// метод структуры меняет this, но переменная-копия — отдельное значение
var b = new Box();
var copy = b;
copy.Set(2);
Console.WriteLine(b.N); // 0

// Метод к чужому типу — только extension-метод
Console.WriteLine(100.0.ToFahrenheit()); // 212

class Counter(string name)
{
    private readonly string _name = name.ToUpper();
    private int _hits;
    public void Hit() => _hits++;
    public override string ToString() => $"{_name}={_hits}";
}

struct Box
{
    public int N;
    public void Set(int n) => N = n;
}

static class TemperatureExtensions
{
    public static double ToFahrenheit(this double celsius) => celsius * 9 / 5 + 32;
}
