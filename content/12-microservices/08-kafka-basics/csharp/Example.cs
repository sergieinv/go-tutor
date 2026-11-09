using System.Text;
using System.Text.Json;
using Confluent.Kafka;
using Confluent.Kafka.Admin;

// NuGet-пакет Confluent.Kafka — обёртка над нативной librdkafka.
var brokers = Environment.GetEnvironmentVariable("KAFKA_BROKERS");
if (string.IsNullOrEmpty(brokers))
{
    Console.WriteLine("нужна Kafka: docker compose --profile labs up");
    return;
}
const string topic = "orders.events";

// Админ-клиент — отдельный объект (в Go — kadm поверх того же kgo.Client).
using (var admin = new AdminClientBuilder(new AdminClientConfig { BootstrapServers = brokers }).Build())
{
    await admin.CreateTopicsAsync([
        new TopicSpecification
        {
            Name = topic, NumPartitions = 3, ReplicationFactor = 1,
            Configs = new() { ["retention.ms"] = "604800000" },
        },
    ]);
}

var config = new ProducerConfig
{
    BootstrapServers = brokers,
    // По умолчанию librdkafka — consistent_random (CRC32): тот же ключ, что из Go,
    // может уйти в другую партицию. murmur2 совместим с Java-клиентом и franz-go.
    Partitioner = Partitioner.Murmur2Random,
};
// Сериализатор значения — часть клиента; в Go Value — уже []byte.
using var producer = new ProducerBuilder<string, OrderEvent>(config)
    .SetValueSerializer(new JsonValueSerializer<OrderEvent>())
    .Build();

// ProduceAsync ↔ ProduceSync: ждём подтверждения брокера.
var result = await producer.ProduceAsync(topic, new Message<string, OrderEvent>
{
    Key = "A-1", // ключ — id заказа: порядок внутри партиции
    Value = new OrderEvent("A-1", "created"),
    Headers = new Headers
    {
        { "event-type", Encoding.UTF8.GetBytes("created") },
        { "content-type", Encoding.UTF8.GetBytes("application/json") },
    },
});
Console.WriteLine($"p{result.Partition.Value}@{result.Offset.Value}");

// Produce с колбэком ↔ cl.Produce(ctx, rec, promise); Flush перед выходом обязателен.
producer.Produce(topic, new Message<string, OrderEvent> { Key = "B-2", Value = new OrderEvent("B-2", "created") },
    report => Console.WriteLine(report.Error.IsError ? report.Error.Reason : $"p{report.Partition.Value}"));
producer.Flush(TimeSpan.FromSeconds(10));

public record OrderEvent(string OrderId, string Type);

public class JsonValueSerializer<T> : ISerializer<T>
{
    public byte[] Serialize(T data, SerializationContext context) => JsonSerializer.SerializeToUtf8Bytes(data);
}
