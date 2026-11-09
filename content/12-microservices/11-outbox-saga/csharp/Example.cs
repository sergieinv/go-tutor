// MassTransit (NuGet MassTransit.EntityFrameworkCore) делает outbox и саги конфигурацией:
//   x.AddEntityFrameworkOutbox<OrdersDb>(o => o.UsePostgres().UseBusOutbox());
//   x.AddSagaStateMachine<OrderStateMachine, OrderState>().EntityFrameworkRepository(...);
// Ниже — оркестратор саги руками, как в задании: состояние сохраняется после каждого шага.

var store = new MemoryStore();
var saga = new SagaRunner(store);
Step[] steps =
[
    new("склад: резерв", _ => Task.CompletedTask, _ => { Console.WriteLine("резерв отменён"); return Task.CompletedTask; }),
    new("заказы: создать", _ => Task.CompletedTask, _ => { Console.WriteLine("заказ отменён"); return Task.CompletedTask; }),
    new("оплата: списать", _ => throw new InvalidOperationException("карта отклонена"), null),
];
var status = await saga.RunAsync("saga-1", steps, CancellationToken.None);
Console.WriteLine(status);                                                   // Compensated
Console.WriteLine(await saga.RunAsync("saga-1", steps, CancellationToken.None)); // повтор ничего не делает

public enum SagaStatus { None, Running, Completed, Compensating, Compensated, Failed }

public record Step(string Name, Func<string, Task> Do, Func<string, Task>? Compensate);

public record SagaState(SagaStatus Status, IReadOnlyList<string> Done);

public interface ISagaStore
{
    Task<SagaState> LoadAsync(string sagaId, CancellationToken ct);
    Task SaveAsync(string sagaId, SagaState state, CancellationToken ct);
}

public class MemoryStore : ISagaStore
{
    private readonly Dictionary<string, SagaState> _states = new();

    public Task<SagaState> LoadAsync(string sagaId, CancellationToken ct) =>
        Task.FromResult(_states.GetValueOrDefault(sagaId, new SagaState(SagaStatus.None, [])));

    public Task SaveAsync(string sagaId, SagaState state, CancellationToken ct)
    {
        _states[sagaId] = state with { Done = state.Done.ToList() };
        return Task.CompletedTask;
    }
}

public class SagaRunner(ISagaStore store)
{
    public async Task<SagaStatus> RunAsync(string sagaId, Step[] steps, CancellationToken ct)
    {
        var st = await store.LoadAsync(sagaId, ct);
        if (st.Status is SagaStatus.Completed or SagaStatus.Compensated) return st.Status;
        var done = st.Done.ToList();

        if (st.Status is SagaStatus.None or SagaStatus.Running)
        {
            try
            {
                foreach (var s in steps.Where(s => !done.Contains(s.Name)))
                {
                    await s.Do(sagaId);
                    done.Add(s.Name);
                    await store.SaveAsync(sagaId, new SagaState(SagaStatus.Running, done), ct);
                }
                await store.SaveAsync(sagaId, new SagaState(SagaStatus.Completed, done), ct);
                return SagaStatus.Completed;
            }
            catch (InvalidOperationException) // упал шаг (в Go — возвращённая ошибка)
            {
                await store.SaveAsync(sagaId, new SagaState(SagaStatus.Compensating, done), ct);
            }
        }

        // Компенсации — в обратном порядке.
        while (done.Count > 0)
        {
            var step = steps.Single(s => s.Name == done[^1]);
            try
            {
                if (step.Compensate is not null) await step.Compensate(sagaId);
            }
            catch
            {
                await store.SaveAsync(sagaId, new SagaState(SagaStatus.Failed, done), ct);
                return SagaStatus.Failed; // полууспех: алерт, ручной разбор
            }
            done.RemoveAt(done.Count - 1);
            await store.SaveAsync(sagaId, new SagaState(SagaStatus.Compensating, done), ct);
        }
        await store.SaveAsync(sagaId, new SagaState(SagaStatus.Compensated, done), ct);
        return SagaStatus.Compensated;
    }
}
