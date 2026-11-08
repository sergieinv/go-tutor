using System.Data.Common;

// Провайдер (Microsoft.Data.Sqlite, Npgsql) подключается из NuGet; здесь только
// абстракции System.Data.Common, чтобы код собирался без пакетов.
DbConnection? conn = null;
if (conn is not null)
{
    using var cmd = Users.Find(conn, "' OR 1=1 --"); // найдёт только такое имя буквально
    Console.WriteLine(cmd.ExecuteScalar());
}

static class Users
{
    // УЯЗВИМО: интерполяция — та же склейка, ввод становится текстом SQL.
    public static DbCommand FindUnsafe(DbConnection conn, string name)
    {
        var cmd = conn.CreateCommand();
        cmd.CommandText = $"SELECT name FROM users WHERE name = '{name}'";
        return cmd;
    }

    // Параметр: текст постоянный, значение уходит отдельно — как ? и args в Go.
    public static DbCommand Find(DbConnection conn, string name)
    {
        var cmd = conn.CreateCommand();
        cmd.CommandText = "SELECT name FROM users WHERE name = @name";
        var p = cmd.CreateParameter();
        p.ParameterName = "@name";
        p.Value = name;
        cmd.Parameters.Add(p);
        return cmd;
    }

    // ORDER BY параметром не передать ни в ADO.NET, ни в Go — allowlist.
    static readonly Dictionary<string, string> SortSql = new()
    {
        ["name"] = "name", ["-name"] = "name DESC", ["age"] = "age", ["-age"] = "age DESC",
    };

    public static DbCommand List(DbConnection conn, string sort)
    {
        if (!SortSql.TryGetValue(sort, out var order))
            throw new ArgumentException("недопустимая сортировка", nameof(sort));
        var cmd = conn.CreateCommand();
        cmd.CommandText = "SELECT name FROM users ORDER BY " + order + ", id";
        return cmd;
    }

    // IN (...) — по параметру на значение; Dapper разворачивает список сам,
    // в Go и чистом ADO.NET — руками.
    public static DbCommand ByIds(DbConnection conn, IReadOnlyList<long> ids)
    {
        var cmd = conn.CreateCommand();
        var names = new List<string>();
        for (var i = 0; i < ids.Count; i++)
        {
            var p = cmd.CreateParameter();
            p.ParameterName = $"@id{i}";
            p.Value = ids[i];
            cmd.Parameters.Add(p);
            names.Add(p.ParameterName);
        }
        cmd.CommandText = $"SELECT name FROM users WHERE id IN ({string.Join(",", names)})";
        return cmd;
    }
}

// EF Core: FromSqlInterpolated($"... {name}") превращает интерполяцию в параметры,
// а FromSqlRaw("... '" + name + "'") — нет. В Go такой «умной» строки нет:
// db.QueryContext(ctx, fmt.Sprintf(...)) всегда склейка.
