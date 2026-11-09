// В .NET circuit breaker почти всегда берут из Polly v8:
//
//   var pipeline = new ResiliencePipelineBuilder()
//       .AddCircuitBreaker(new CircuitBreakerStrategyOptions
//       {
//           FailureRatio = 0.5,                          // доля ошибок…
//           SamplingDuration = TimeSpan.FromSeconds(30), // …в окне 30 с
//           MinimumThroughput = 20,                      // …если вызовов не меньше 20
//           BreakDuration = TimeSpan.FromSeconds(5),     // сколько быть open
//           ShouldHandle = new PredicateBuilder().Handle<RpcException>(e =>
//               e.StatusCode is StatusCode.Unavailable or StatusCode.DeadlineExceeded),
//       })
//       .Build();
//   // Открытый breaker бросает BrokenCircuitException.
//
// Ниже — минимальный breaker «неудач подряд» без NuGet, как в задании урока.
// Время — TimeProvider (.NET 8+): в тестах его подменяют FakeTimeProvider.

public sealed class BrokenCircuitException() : Exception("circuit breaker is open");

public enum CircuitState { Closed, Open, HalfOpen }

public sealed class Breaker(int threshold, TimeSpan openFor, TimeProvider time)
{
    readonly object _gate = new(); // с C# 13 (.NET 9+) — System.Threading.Lock
    CircuitState _state = CircuitState.Closed;
    long _generation;
    int _failures;
    bool _probing;
    DateTimeOffset _openedAt;

    public CircuitState State
    {
        get { lock (_gate) { Refresh(); return _state; } }
    }

    public async Task<T> ExecuteAsync<T>(Func<Task<T>> call, Func<Exception, bool> isFailure)
    {
        long gen;
        lock (_gate)
        {
            Refresh();
            if (_state == CircuitState.Open || (_state == CircuitState.HalfOpen && _probing))
                throw new BrokenCircuitException();
            if (_state == CircuitState.HalfOpen) _probing = true;
            gen = _generation;
        }
        try
        {
            var result = await call(); // без блокировки, как fn в Go
            Record(gen, failed: false);
            return result;
        }
        catch (Exception e)
        {
            Record(gen, isFailure(e));
            throw;
        }
    }

    void Record(long gen, bool failed)
    {
        lock (_gate)
        {
            if (gen != _generation) return; // итог из прошлого поколения
            if (_state == CircuitState.HalfOpen)
                Set(failed ? CircuitState.Open : CircuitState.Closed);
            else if (!failed)
                _failures = 0;
            else if (++_failures >= threshold)
                Set(CircuitState.Open);
        }
    }

    void Refresh()
    {
        if (_state == CircuitState.Open && time.GetUtcNow() - _openedAt >= openFor)
            Set(CircuitState.HalfOpen);
    }

    void Set(CircuitState s)
    {
        _state = s;
        _generation++;
        _failures = 0;
        _probing = false;
        if (s == CircuitState.Open) _openedAt = time.GetUtcNow();
    }
}

public static class Program
{
    public static async Task Main()
    {
        var br = new Breaker(3, TimeSpan.FromSeconds(5), TimeProvider.System);
        for (var i = 0; i < 4; i++)
        {
            try
            {
                await br.ExecuteAsync<int>(() => throw new TimeoutException(), _ => true);
            }
            catch (Exception e)
            {
                Console.WriteLine($"{e.GetType().Name}, состояние {br.State}");
            }
        }
    }
}
