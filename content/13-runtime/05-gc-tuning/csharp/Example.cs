using System;
using System.Runtime;

// Режим сборщика задаётся в runtimeconfig.json (или в .csproj):
//   "System.GC.Server": true,           — Server GC, несколько куч (с .NET 9
//                                         их число по умолчанию подбирает DATAS)
//   "System.GC.Concurrent": true,       — фоновая сборка поколения 2
//   "System.GC.HeapHardLimit": 805306368 — жёсткий лимит кучи, 768 МиБ
//   "System.GC.ConserveMemory": 5       — экономить память ценой CPU (0–9)
// Аналога GOGC нет: бюджеты поколений CLR подбирает сам.
Console.WriteLine($"Server GC: {GCSettings.IsServerGC}");

// Во время работы можно сменить только режим задержек.
var old = GCSettings.LatencyMode;
GCSettings.LatencyMode = GCLatencyMode.SustainedLowLatency;
Console.WriteLine($"режим: {GCSettings.LatencyMode}");
GCSettings.LatencyMode = old;

// Метрики без остановки мира — как runtime/metrics.
var info = GC.GetGCMemoryInfo();
Console.WriteLine($"доступно памяти: {info.TotalAvailableMemoryBytes > 0}");
Console.WriteLine($"сборок поколения 0: {GC.CollectionCount(0) >= 0}");

// Жёсткий лимит: если после сборки места нет — исключение.
// OutOfMemoryException здесь будет, только если задан GCHeapHardLimit
// (в этом примере он не задан — выделение пройдёт).
// GOMEMLIMIT в Go мягкий: сборщик работает чаще, но не бросает.
try
{
    var huge = new byte[int.MaxValue / 2];
    Console.WriteLine($"выделено: {huge.Length > 0}");
}
catch (OutOfMemoryException)
{
    Console.WriteLine("OutOfMemoryException");
}
