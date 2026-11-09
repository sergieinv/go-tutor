#pragma warning disable CS0649 // поля нужны только ради размера
using System;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;

// struct в C# по умолчанию Sequential: поля в порядке объявления,
// с выравниванием и дырами — ровно как любая структура в Go.
Console.WriteLine(Unsafe.SizeOf<OrderBad>());   // 24: bool, 7 пустых, long, bool, 7 пустых
Console.WriteLine(Unsafe.SizeOf<OrderGood>());  // 16: long, bool, bool, 6 пустых

// LayoutKind.Auto разрешает рантайму переставить поля самому.
// В Go такого атрибута нет: компилятор порядок не меняет никогда.
Console.WriteLine(Unsafe.SizeOf<OrderAuto>());  // 16

// Pack = 1 убирает выравнивание совсем — для форматов на диске
// и interop. В Go аналога нет: только ручная сериализация в []byte.
Console.WriteLine(Unsafe.SizeOf<OrderPacked>()); // 10

// Массив структур — один непрерывный блок, как []T в Go;
// массив классов — массив ссылок на объекты в куче, как []*T.
var values = new OrderGood[1000];
var refs = new OrderClass[1000];
for (int i = 0; i < refs.Length; i++) refs[i] = new OrderClass { Id = i };
Console.WriteLine(values.Length + refs.Length);

struct OrderBad { public bool Paid; public long Id; public bool Gift; }
struct OrderGood { public long Id; public bool Paid; public bool Gift; }

[StructLayout(LayoutKind.Auto)]
struct OrderAuto { public bool Paid; public long Id; public bool Gift; }

[StructLayout(LayoutKind.Sequential, Pack = 1)]
struct OrderPacked { public bool Paid; public long Id; public bool Gift; }

// Поля класса CLR раскладывает сам (Auto) — порядок в исходнике не важен.
sealed class OrderClass { public bool Paid; public long Id; public bool Gift; }
