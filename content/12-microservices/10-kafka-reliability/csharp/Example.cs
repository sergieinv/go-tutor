using System.Text;
using System.Text.Json;
using Confluent.Kafka;

var brokers = Environment.GetEnvironmentVariable("KAFKA_BROKERS");
if (string.IsNullOrEmpty(brokers))
{
    Console.WriteLine("нужна Kafka: docker compose --profile labs up");
    return;
}

// В librdkafka идемпотентность выключена по умолчанию — включаем явно (в kgo — включена).
var producerConfig = new ProducerConfig
{
    BootstrapServers = brokers,
    EnableIdempotence = true,
    Acks = Acks.All,              // идемпотентность требует acks=all
    MessageTimeoutMs = 120_000,   // ↔ kgo.RecordDeliveryTimeout; по умолчанию 5 минут
};
using var dlqProducer = new ProducerBuilder<string, string>(producerConfig).Build();

var processed = new HashSet<string>(); // в проде — таблица в БД, в одной транзакции с эффектом
var consumer = new Processor(dlqProducer, "orders.events.dlq", processed, maxAttempts: 3);
var result = new ConsumeResult<string, string>
{
    Topic = "orders.events", Partition = 0, Offset = 1,
    Message = new Message<string, string>
    {
        Key = "A-1",
        Value = "{\"order_id\":\"A-1\",",
        Headers = new Headers { { "event-id", Encoding.UTF8.GetBytes("e2") } },
    },
};
Console.WriteLine(await consumer.ProcessAsync(result, CancellationToken.None));

public enum Outcome { Processed, Duplicate, DeadLettered }

public class PermanentException(string message, Exception inner) : Exception(message, inner);

public class Processor(IProducer<string, string> dlq, string dlqTopic, HashSet<string> processed, int maxAttempts)
{
    public async Task<Outcome> ProcessAsync(ConsumeResult<string, string> r, CancellationToken ct)
    {
        var id = r.Message.Headers.TryGetLastBytes("event-id", out var raw) ? Encoding.UTF8.GetString(raw) : "";
        if (id.Length > 0 && processed.Contains(id)) return Outcome.Duplicate;

        Exception? last = null;
        var attempt = 0;
        while (attempt < maxAttempts)
        {
            attempt++;
            try
            {
                Handle(r.Message);
                processed.Add(id);
                return Outcome.Processed;
            }
            catch (PermanentException e) { last = e; break; } // не повторять
            catch (Exception e) { last = e; }                 // здесь — пауза с backoff (Polly)
        }

        var headers = new Headers();
        foreach (var h in r.Message.Headers) headers.Add(h.Key, h.GetValueBytes());
        headers.Add("dlq-reason", Encoding.UTF8.GetBytes(last!.Message));
        headers.Add("dlq-attempts", Encoding.UTF8.GetBytes(attempt.ToString()));
        headers.Add("dlq-original-topic", Encoding.UTF8.GetBytes(r.Topic));
        headers.Add("dlq-original-partition", Encoding.UTF8.GetBytes(r.Partition.Value.ToString()));
        headers.Add("dlq-original-offset", Encoding.UTF8.GetBytes(r.Offset.Value.ToString()));
        // await: исключение — DLQ не принял, исходную запись коммитить нельзя.
        await dlq.ProduceAsync(dlqTopic, new Message<string, string> { Key = r.Message.Key, Value = r.Message.Value, Headers = headers }, ct);
        processed.Add(id);
        return Outcome.DeadLettered;
    }

    static void Handle(Message<string, string> m)
    {
        try
        {
            JsonDocument.Parse(m.Value);
        }
        catch (JsonException e)
        {
            throw new PermanentException("битый JSON", e);
        }
    }
}
