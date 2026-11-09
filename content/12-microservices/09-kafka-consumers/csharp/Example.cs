using Confluent.Kafka;

// Потребитель группы на Confluent.Kafka: ручной коммит после обработки.
var brokers = Environment.GetEnvironmentVariable("KAFKA_BROKERS");
if (string.IsNullOrEmpty(brokers))
{
    Console.WriteLine("нужна Kafka: docker compose --profile labs up");
    return;
}

using var cts = new CancellationTokenSource();
Console.CancelKeyPress += (_, e) => { e.Cancel = true; cts.Cancel(); }; // ↔ signal.NotifyContext

var config = new ConsumerConfig
{
    BootstrapServers = brokers,
    GroupId = "notifications",                 // ↔ kgo.ConsumerGroup
    EnableAutoCommit = false,                  // ↔ kgo.DisableAutoCommit()
    AutoOffsetReset = AutoOffsetReset.Earliest,
    // В librdkafka cooperative-sticky включают явно, в kgo — по умолчанию.
    PartitionAssignmentStrategy = PartitionAssignmentStrategy.CooperativeSticky,
    // Обработка дольше этого без Consume — участника выгоняют из группы. В kgo лимита нет.
    MaxPollIntervalMs = 300_000,
};

using var consumer = new ConsumerBuilder<string, string>(config)
    .SetPartitionsAssignedHandler((_, parts) => Console.WriteLine($"назначены: {string.Join(",", parts)}"))
    .SetPartitionsRevokedHandler((c, parts) =>
    {
        // ↔ OnPartitionsRevoked: успеть закоммитить сделанное до переезда партиций.
        Console.WriteLine($"отозваны: {string.Join(",", parts)}");
    })
    .Build();
consumer.Subscribe("orders.events");                 // ↔ kgo.ConsumeTopics

try
{
    while (!cts.IsCancellationRequested)
    {
        var result = consumer.Consume(cts.Token);    // одна запись; PollFetches — пачка
        Handle(result.Message);                      // исключение — коммита не будет
        consumer.Commit(result);                     // ↔ CommitRecords: офсет + 1
    }
}
catch (OperationCanceledException)
{
    // штатная остановка
}
finally
{
    consumer.Close(); // выход из группы — партиции сразу уходят другим (как cl.Close())
}

static void Handle(Message<string, string> m) => Console.WriteLine($"{m.Key}: {m.Value}");
