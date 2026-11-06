// params — аналог ...int
static int Sum(params int[] nums) => nums.Sum();

static void Double(params int[] nums)
{
    for (var i = 0; i < nums.Length; i++) nums[i] *= 2;
}

Console.WriteLine(Sum(1, 2, 3));
Console.WriteLine(Sum());        // пустой массив, а не null

// Массив передаётся в params без разворота и без копии —
// как sum(s...) в Go
var arr = new[] { 10, 20, 30 };
Console.WriteLine(Sum(arr));
Double(arr);
Console.WriteLine(string.Join(",", arr)); // 20,40,60 — изменён

// Отдельные аргументы — новый массив, как и в Go
int a = 1, b = 2;
Double(a, b);
Console.WriteLine($"{a} {b}");   // 1 2

// params object[] — как ...any; значимые типы упаковываются
Console.WriteLine(string.Format("{0}={1}, ok={2}", "x", 42, true));
