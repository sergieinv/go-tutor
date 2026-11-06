// ref — передача переменной по ссылке; в Go — incPtr(&x)
static void Inc(ref int n) => n++;

var x = 1;
Inc(ref x);
Console.WriteLine(x); // 2

// Класс — ссылочный тип: присваивание копирует ссылку
var a = new PointClass { X = 1 };
var b = a;
b.X = 100;
Console.WriteLine(a.X); // 100

// Структура — значимый тип: копируется целиком.
// В Go так ведёт себя ЛЮБОЙ тип, а разделяют его через *T.
var s1 = new PointStruct { X = 1 };
var s2 = s1;
s2.X = 100;
Console.WriteLine(s1.X); // 1

// ref на элемент массива — как &nums[1] в Go
var nums = new[] { 5, 6, 7 };
ref int second = ref nums[1];
second *= 10;
Console.WriteLine(string.Join(",", nums)); // 5,60,7

// null-ссылка — NullReferenceException; в Go — panic
PointClass? none = null;
try { Console.WriteLine(none!.X); }
catch (NullReferenceException) { Console.WriteLine("NRE"); }

class PointClass { public int X; }
struct PointStruct { public int X; }
