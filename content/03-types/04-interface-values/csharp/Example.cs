object v = "go";

// is-шаблон — аналог v.(T) с ok
if (v is string s)
    Console.WriteLine(s.Length);

// Приведение без проверки — аналог v.(T) без ok: InvalidCastException
try { var n = (int)v; }
catch (InvalidCastException) { Console.WriteLine("не int"); }

// switch по типу — аналог type switch
Console.WriteLine(v switch
{
    null => "null",
    int i => $"int {i + 1}",
    string or byte[] => "текст",
    Exception e => "ошибка: " + e.Message,
    _ => "что-то ещё",
});

// В C# ссылка либо null, либо нет — «null внутри не-null» не бывает
Exception? err = Find("user");
Console.WriteLine(err == null); // True

static Exception? Find(string key)
{
    KeyNotFoundException? e = null;
    return e; // всё ещё null
}
