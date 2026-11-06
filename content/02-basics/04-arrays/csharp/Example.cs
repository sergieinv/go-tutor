using System.Runtime.CompilerServices;

int[] a = [1, 2, 3];
int[] b = a;          // копируется ссылка, массив один
b[0] = 100;
Console.WriteLine(a[0]);              // 100
Console.WriteLine(a == new[] { 100, 2, 3 });          // False — сравнение ссылок
Console.WriteLine(a.SequenceEqual([100, 2, 3]));      // True

// Длина не входит в тип: int[] любой длины — один тип
int[] longer = new int[10];
b = longer;

// Ближайший аналог массива Go — InlineArray (C# 12):
// фиксированный размер и копирование при присваивании.
var v = new Buffer3();
v[0] = 1;
var w = v;            // копия всех элементов
w[0] = 2;
Console.WriteLine(v[0]);              // 1

[InlineArray(3)]
struct Buffer3 { private int _e0; }
