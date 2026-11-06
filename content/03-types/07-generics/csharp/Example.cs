int[] nums = [1, 2, 3, 4];

// LINQ — generic-методы расширения с собственными параметрами типов:
// в Go 1.25 цепочку Select<TResult> на типе не построить, Map — функция пакета
var strs = nums.Select(n => n.ToString()).ToList();
Console.WriteLine(string.Join("+", strs));

var s = new Stack<string>();
s.Push("a");
Console.WriteLine(s.Pop());

// Вариантность: IEnumerable<out T> позволяет так, в Go []Dog — не []Animal
IEnumerable<object> objs = strs;

// default(T) — в Go: var zero T
Console.WriteLine(Zero<int>());

// Ограничение new() и вызов конструктора T — в Go такого нет
static T Create<T>() where T : new() => new T();
static T? Zero<T>() => default;
