using System;
using System.Runtime;
using System.Runtime.InteropServices;

// CLR: поколения и уплотнение. Новый объект — в поколении 0,
// пережив сборки, он переезжает в 1 и 2.
var obj = new byte[100];
Console.WriteLine($"поколение: {GC.GetGeneration(obj)}");
GC.Collect();
Console.WriteLine($"после сборки: {GC.GetGeneration(obj)}");

// Большие объекты (от 85 000 байт) сразу в LOH — он логически
// относится ко второму поколению.
var big = new byte[100_000];
Console.WriteLine($"LOH: {GC.GetGeneration(big)}");

// Сборщик двигает объекты, поэтому для передачи адреса
// в нативный код объект закрепляют. В Go объекты неподвижны.
var handle = GCHandle.Alloc(obj, GCHandleType.Pinned);
IntPtr addr = handle.AddrOfPinnedObject();
Console.WriteLine($"закреплён: {addr != IntPtr.Zero}");
handle.Free();

// Слабая ссылка — как weak.Pointer[T] в Go.
var weak = MakeWeak();
GC.Collect();
Console.WriteLine($"после сборки жив: {weak.TryGetTarget(out _)}");

// Режим сборщика: Server/Workstation и конкурентность задаются
// в runtimeconfig.json, а не в коде.
Console.WriteLine($"Server GC: {GCSettings.IsServerGC}");

// Детерминированное освобождение — using/IDisposable,
// как defer f.Close() в Go; финализатор — страховка.
using (var res = new Resource()) { }

static WeakReference<object> MakeWeak() => new(new object());

sealed class Resource : IDisposable
{
    public void Dispose() => Console.WriteLine("ресурс закрыт");
}
