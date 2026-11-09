using System.Diagnostics;
using System.Text;

// В .NET трассировка встроена в System.Diagnostics: ActivitySource ≈ Tracer,
// Activity ≈ Span. OpenTelemetry .NET только подписывается на них и экспортирует:
//   builder.Services.AddOpenTelemetry()
//       .ConfigureResource(r => r.AddService("orders"))
//       .WithTracing(t => t.AddAspNetCoreInstrumentation()
//                          .AddGrpcClientInstrumentation()
//                          .AddSource("orders")
//                          .AddOtlpExporter());
// Без NuGet роль SDK здесь играет ActivityListener.
// В Go API (otel/trace) и SDK (otel/sdk/trace) — отдельные модули,
// а контекст спана едет в context.Context, а не в AsyncLocal (Activity.Current).
var source = new ActivitySource("orders");
var finished = new List<Activity>();
ActivitySource.AddActivityListener(new ActivityListener
{
    ShouldListenTo = _ => true,
    Sample = (ref ActivityCreationOptions<ActivityContext> _) => ActivitySamplingResult.AllDataAndRecorded,
    ActivityStopped = finished.Add,
});

// Продюсер: спан публикации и traceparent в заголовки записи Kafka
// (у Confluent.Kafka это Headers — список пар string → byte[]).
var headers = new List<KeyValuePair<string, byte[]>>();
using (var publish = source.StartActivity("publish orders.events", ActivityKind.Producer))
{
    publish?.SetTag("messaging.system", "kafka");
    DistributedContextPropagator.Current.Inject(publish, headers, static (carrier, key, value) =>
        ((List<KeyValuePair<string, byte[]>>)carrier!).Add(new(key, Encoding.UTF8.GetBytes(value))));
}

// Потребитель: достаём traceparent и продолжаем трассу.
// В Go то же делают propagation.TraceContext{}.Extract и свой TextMapCarrier.
DistributedContextPropagator.Current.ExtractTraceIdAndState(headers,
    static (object? carrier, string key, out string? value, out IEnumerable<string>? values) =>
    {
        values = null;
        var h = ((List<KeyValuePair<string, byte[]>>)carrier!).FirstOrDefault(p => p.Key == key);
        value = h.Value is null ? null : Encoding.UTF8.GetString(h.Value);
    },
    out var traceParent, out var traceState);

if (ActivityContext.TryParse(traceParent, traceState, isRemote: true, out var parent))
{
    using var process = source.StartActivity("process orders.events", ActivityKind.Consumer, parent);
    try
    {
        throw new InvalidOperationException("bad payload");
    }
    catch (Exception ex)
    {
        // span.RecordError + span.SetStatus(codes.Error, ...) в Go.
        process?.AddException(ex);
        process?.SetStatus(ActivityStatusCode.Error, ex.Message);
    }
}

foreach (var a in finished)
    Console.WriteLine($"{a.DisplayName} [{a.Kind}] parent={(a.ParentSpanId == default ? "-" : "есть")} status={a.Status}");
