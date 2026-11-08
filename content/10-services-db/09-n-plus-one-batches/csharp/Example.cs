using System.Data.Common;
using System.Text;

// В EF Core стратегию загрузки выбирают через LINQ (нужен NuGet, поэтому в комментарии):
//   foreach (var o in db.Orders) Console.WriteLine(o.Items.Count);   // lazy loading: N+1
//   db.Orders.Include(o => o.Items).ToListAsync(ct);                  // один JOIN
//   db.Orders.Include(o => o.Items).AsSplitQuery().ToListAsync(ct);   // второй запрос по ключам
// Ниже — вариант AsSplitQuery и пакетная вставка руками на ADO.NET, как в Go-примере.

DbDataSource? source = null; // NpgsqlDataSource.Create(...) в настоящем приложении
if (source is not null)
{
    var repo = new OrderRepo(source);
    var items = await repo.ItemsByOrdersAsync([3, 1, 3], CancellationToken.None);
    Console.WriteLine(items.Count);
}

public record Item(string Sku, long Qty);

public class OrderRepo(DbDataSource db)
{
    const int MaxIdsPerQuery = 500;

    // Позиции многих заказов: запрос на пачку id, а не на каждый заказ.
    public async Task<Dictionary<long, List<Item>>> ItemsByOrdersAsync(IEnumerable<long> ids, CancellationToken ct)
    {
        var result = new Dictionary<long, List<Item>>();
        foreach (var chunk in ids.Distinct().Order().Chunk(MaxIdsPerQuery)) // Chunk — как slices.Chunk
        {
            await using var cmd = db.CreateCommand();
            // Текст — только из имён параметров, значения идут отдельно: как "?, ?, ?" в Go.
            var names = new StringBuilder();
            for (var i = 0; i < chunk.Length; i++)
            {
                var p = cmd.CreateParameter();
                p.ParameterName = $"@id{i}";
                p.Value = chunk[i];
                cmd.Parameters.Add(p);
                names.Append(i == 0 ? "" : ", ").Append(p.ParameterName);
            }
            cmd.CommandText = $"SELECT order_id, sku, qty FROM items WHERE order_id IN ({names}) ORDER BY id";

            await using var r = await cmd.ExecuteReaderAsync(ct);
            while (await r.ReadAsync(ct))
            {
                var orderId = r.GetInt64(0);
                if (!result.TryGetValue(orderId, out var list))
                    result[orderId] = list = [];
                list.Add(new Item(r.GetString(1), r.GetInt64(2)));
            }
        }
        return result;
    }

    // Многострочный INSERT пачками; транзакцию передаёт вызывающий, как *sql.Tx в Go.
    public static async Task InsertItemsAsync(DbConnection conn, DbTransaction tx, long orderId, IReadOnlyList<Item> items, CancellationToken ct)
    {
        const int maxParams = 999, perRow = 3;
        foreach (var chunk in items.Chunk(maxParams / perRow))
        {
            await using var cmd = conn.CreateCommand();
            cmd.Transaction = tx;
            var values = new StringBuilder();
            for (var i = 0; i < chunk.Length; i++)
            {
                values.Append(i == 0 ? "" : ", ").Append($"(@o{i}, @s{i}, @q{i})");
                Add(cmd, $"@o{i}", orderId);
                Add(cmd, $"@s{i}", chunk[i].Sku);
                Add(cmd, $"@q{i}", chunk[i].Qty);
            }
            cmd.CommandText = $"INSERT INTO items (order_id, sku, qty) VALUES {values}";
            await cmd.ExecuteNonQueryAsync(ct);
        }
    }

    static void Add(DbCommand cmd, string name, object value)
    {
        var p = cmd.CreateParameter();
        p.ParameterName = name;
        p.Value = value;
        cmd.Parameters.Add(p);
    }
}
