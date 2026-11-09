using System.Diagnostics;
using System.Diagnostics.Metrics;

// Два пути в .NET.
// prometheus-net — прямой аналог client_golang:
//   var created = Metrics.CreateCounter("orders_created_total", "Созданные заказы.");
//   app.UseHttpMetrics();   // RED для всех запросов ASP.NET Core
//   app.MapMetrics();       // GET /metrics
// System.Diagnostics.Metrics + OpenTelemetry — встроенный API (как OTel metrics в Go):
//   builder.Services.AddOpenTelemetry().WithMetrics(m => m
//       .AddAspNetCoreInstrumentation().AddMeter("orders").AddPrometheusExporter());
// Ниже — встроенный Meter; MeterListener вместо экспортёра, чтобы собраться без NuGet.
var meter = new Meter("orders");
var requests = meter.CreateCounter<long>("http.server.requests");
var duration = meter.CreateHistogram<double>("http.server.request.duration", unit: "s");
var inFlight = meter.CreateUpDownCounter<long>("http.server.active_requests");

var listener = new MeterListener
{
    InstrumentPublished = (instrument, l) =>
    {
        if (instrument.Meter.Name == "orders") l.EnableMeasurementEvents(instrument);
    },
};
listener.SetMeasurementEventCallback<long>((inst, value, tags, _) =>
    Console.WriteLine($"{inst.Name} {value:+#;-#} {string.Join(",", tags.ToArray().Select(t => $"{t.Key}={t.Value}"))}"));
listener.SetMeasurementEventCallback<double>((inst, value, tags, _) =>
    Console.WriteLine($"{inst.Name} {value:0.###}s"));
listener.Start();

// «Middleware»: те же три метрики, что в Go-примере. Метка маршрута —
// шаблон ("GET /orders/{id}"), а не путь: в ASP.NET Core его даёт
// endpoint routing, в Go — r.Pattern.
void Handle(string route, Func<int> handler)
{
    inFlight.Add(1);
    var sw = Stopwatch.StartNew();
    try
    {
        var code = handler();
        requests.Add(1, new("route", route), new("code", code));
    }
    finally
    {
        duration.Record(sw.Elapsed.TotalSeconds, new KeyValuePair<string, object?>("route", route));
        inFlight.Add(-1);
    }
}

Handle("POST /orders", () => 201);
Handle("GET /orders/{id}", () => 404);
