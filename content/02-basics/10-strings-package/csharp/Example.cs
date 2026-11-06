using System.Text;

var s = "  Go is fun, Go is fast  ";

// Методы строки — в Go это функции пакета strings
Console.WriteLine(s.Contains("fun"));                 // strings.Contains
Console.WriteLine(s.Trim());                          // strings.TrimSpace
Console.WriteLine(s.Trim().StartsWith("Go"));         // strings.HasPrefix
Console.WriteLine(s.Replace("Go", "Rust"));           // strings.ReplaceAll
Console.WriteLine(string.Equals("ПРИВЕТ", "привет",
    StringComparison.OrdinalIgnoreCase));             // strings.EqualFold

// Split(null) делит по любым пробельным символам, как strings.Fields
var parts = " a \t b\n".Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries);
Console.WriteLine(string.Join("-", parts));           // strings.Join

// StringBuilder — strings.Builder
var sb = new StringBuilder();
for (var i = 0; i < 3; i++) sb.Append($"item{i} ");
Console.WriteLine(sb.ToString());

// Parse бросает исключение, TryParse возвращает bool.
// В Go strconv.Atoi возвращает (int, error).
try { int.Parse("12a"); }
catch (FormatException e) { Console.WriteLine(e.Message); }

if (int.TryParse("123", out var n)) Console.WriteLine(n + 1);
Console.WriteLine(42.ToString() + "!");               // strconv.Itoa
