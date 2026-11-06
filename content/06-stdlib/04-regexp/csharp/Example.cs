using System;
using System.Linq;
using System.Text.RegularExpressions;

var text = "Релиз 2024-03-15, хотфикс 2024-04-02. #go #release";

// По умолчанию — бэктрекинг; для недоверенного ввода нужен таймаут
// или RegexOptions.NonBacktracking (.NET 7+) — аналог RE2 в Go.
var date = new Regex(@"(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})",
    RegexOptions.Compiled, TimeSpan.FromMilliseconds(100));

foreach (Match m in date.Matches(text))
    Console.WriteLine($"{m.Groups["year"].Value} {m.Groups["month"].Value}");

Console.WriteLine(date.Replace(text, "${day}.${month}.${year}"));

// MatchEvaluator получает Match с группами — в Go ReplaceAllStringFunc получает только строку
Console.WriteLine(Regex.Replace(text, @"#(\w+)", m => "#" + m.Groups[1].Value.ToUpper()));

// Обратные ссылки и lookbehind в .NET есть, в Go (RE2) — нет
Console.WriteLine(Regex.IsMatch("aa", @"(\w)\1"));        // True
Console.WriteLine(Regex.Match("$42", @"(?<=\$)\d+").Value); // 42
