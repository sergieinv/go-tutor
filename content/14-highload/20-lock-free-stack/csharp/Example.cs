using System;
using System.Collections.Concurrent;
using System.Threading;
using System.Threading.Tasks;

// В BCL стек Трайбера уже есть: ConcurrentStack<T> — связный список
// с CAS на вершине, PushRange кладёт пачку одной операцией.
var stack = new ConcurrentStack<int>();
stack.PushRange(new[] { 1, 2, 3 });            // 3 окажется на вершине
stack.TryPop(out var top);
Console.WriteLine(top);                        // 3

// Тот же алгоритм руками — как Push/Pop в примере на Go.
var mine = new TreiberStack<int>();
Parallel.For(0, 1000, i => mine.Push(i));
int count = 0;
while (mine.TryPop(out _)) count++;
Console.WriteLine(count);                      // 1000

sealed class TreiberStack<T>
{
    private sealed class Node(T value, Node? next)
    {
        public readonly T Value = value;
        public Node? Next = next;
    }

    private Node? _head;

    public void Push(T value)
    {
        var node = new Node(value, null);
        while (true)
        {
            var old = Volatile.Read(ref _head);
            node.Next = old;
            // CompareExchange возвращает прежнее значение; совпало — CAS прошёл.
            if (Interlocked.CompareExchange(ref _head, node, old) == old) return;
        }
    }

    public bool TryPop(out T value)
    {
        while (true)
        {
            var old = Volatile.Read(ref _head);
            if (old is null)
            {
                value = default!;
                return false;
            }
            // Как и в Go, GC не переиспользует узел, пока на него есть ссылка
            // old, — ABA на ссылках не возникает.
            if (Interlocked.CompareExchange(ref _head, old.Next, old) == old)
            {
                value = old.Value;
                return true;
            }
        }
    }
}
