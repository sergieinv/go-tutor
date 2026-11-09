using System.Globalization;
using System.Text.RegularExpressions;
using Npgsql;

// Партиционирование — свойство PostgreSQL, а не драйвера и не ORM: EF Core
// его не моделирует, DDL пишут руками (в миграции — migrationBuilder.Sql(...))
// или выполняют из фонового задания, как здесь.
var connString = Environment.GetEnvironmentVariable("POSTGRES_DSN_NPGSQL");
if (connString is null)
{
    Console.WriteLine("нужна строка подключения Npgsql");
    return;
}
await using var db = NpgsqlDataSource.Create(connString);

// Партиции на три месяца вперёд — обычно это делает задание по расписанию
// (в PostgreSQL — расширение pg_partman или pg_cron).
var month = new DateTime(DateTime.UtcNow.Year, DateTime.UtcNow.Month, 1, 0, 0, 0, DateTimeKind.Utc);
for (var i = 0; i < 3; i++)
{
    var from = month.AddMonths(i);
    await using var cmd = db.CreateCommand(CreatePartition("orders", from, from.AddMonths(1)));
    await cmd.ExecuteNonQueryAsync();
}

// Идентификатор в DDL параметром не передать — только белый список, как в Go.
static string CreatePartition(string parent, DateTime from, DateTime to)
{
    // \A и \z, а не ^ и $: $ пропускает перевод строки в конце ("orders\n").
    if (!Regex.IsMatch(parent, @"\A[a-z_][a-z0-9_]{0,54}\z"))
        throw new ArgumentException("плохое имя таблицы", nameof(parent));
    var name = $"{parent}_{from:yyyy_MM}";
    // Явное смещение +00: без него граница зависит от TimeZone сессии.
    string Lit(DateTime t) => t.ToUniversalTime().ToString("yyyy-MM-dd HH:mm:ss", CultureInfo.InvariantCulture) + "+00";
    return $"CREATE TABLE IF NOT EXISTS {name} PARTITION OF {parent} " +
           $"FOR VALUES FROM ('{Lit(from)}') TO ('{Lit(to)}')";
}

// SQL Server для сравнения: partition function + partition scheme и
// ALTER TABLE ... SWITCH PARTITION вместо DETACH — та же идея «отрезать
// старые данные метаданными, а не DELETE».
