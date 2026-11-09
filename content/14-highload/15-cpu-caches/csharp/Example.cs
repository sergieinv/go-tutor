using System;
using System.Collections.Generic;

// Queue<T> в .NET — кольцевой буфер на массиве (то, что просит
// задание урока), LinkedList<T> — узел-объект в куче на каждый элемент,
// как container/list в Go.
var ring = new Queue<long>();
var linked = new LinkedList<long>();
for (long i = 0; i < 1000; i++)
{
    ring.Enqueue(i);       // запись в массив, рост — удвоением
    linked.AddLast(i);     // new LinkedListNode<long> на каждый вызов
}
Console.WriteLine(ring.Dequeue() + linked.First!.Value);

// Матрица в одном массиве по строкам — как []int64 с индексом i*n+j.
// Многомерный long[n, n] в .NET тоже хранится по строкам.
const int n = 512;
var m = new long[n * n];
long rows = 0, cols = 0;
for (int i = 0; i < n; i++)          // по строкам: подряд по памяти
    for (int j = 0; j < n; j++)
        rows += m[i * n + j];
for (int j = 0; j < n; j++)          // по столбцам: шаг n*8 байт
    for (int i = 0; i < n; i++)
        cols += m[i * n + j];
Console.WriteLine(rows == cols);

// Span<T> по куску массива — без копий, как подсрез m[i*n:(i+1)*n].
long sum = 0;
foreach (var x in m.AsSpan(0, n)) sum += x;
Console.WriteLine(sum);
