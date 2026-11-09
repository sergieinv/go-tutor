#pragma warning disable CS0649 // поля нужны только ради раскладки
using System;
using System.Collections;
using System.Collections.Generic;

// Массив структур без ссылок GC не просматривает и в .NET:
// у такого типа нет полей-ссылок, которые надо обходить.
var items = new Item[1_000_000];          // один объект, без ссылок внутри
var boxed = new ItemClass[1_000_000];     // миллион ссылок для GC
for (int i = 0; i < 1000; i++) boxed[i] = new ItemClass { Id = i };
Console.WriteLine(items.Length + boxed.Length);

// Интернирование: string.Intern — пул на весь процесс, навсегда.
// В Go unique.Make: канонические копии слабые и собираются GC,
// когда на них больше нет Handle.
string a = string.Intern(new string('x', 3));
string b = string.Intern("xxx");
Console.WriteLine(ReferenceEquals(a, b));

// Индексы вместо ссылок: дерево в массиве структур.
var tree = new[] { new Node(50, 1, 2), new Node(30, -1, -1), new Node(70, -1, -1) };
Console.WriteLine(Find(tree, 70));

// Готовые структуры: BitArray ↔ bitset на []uint64,
// PriorityQueue<T, P> ↔ container/heap, Array.BinarySearch ↔ slices.BinarySearch.
var bits = new BitArray(128);
bits[100] = true;
var pq = new PriorityQueue<string, int>();
pq.Enqueue("low", 5);
pq.Enqueue("high", 1);
var sorted = new long[] { 3, 8, 15, 42 };
Console.WriteLine($"{bits[100]} {pq.Dequeue()} {Array.BinarySearch(sorted, 15L)}");

static bool Find(Node[] tree, long key)
{
    for (int i = 0; i >= 0;)
    {
        var n = tree[i];
        if (key == n.Key) return true;
        i = key < n.Key ? n.Left : n.Right;
    }
    return false;
}

readonly record struct Node(long Key, int Left, int Right);
struct Item { public long Id; public long Price; }
sealed class ItemClass { public long Id; public long Price; }
