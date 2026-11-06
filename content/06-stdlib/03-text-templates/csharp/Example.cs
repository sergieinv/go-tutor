using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;

var order = new Order(42, "Анна", new List<Item>
{
    new("Кофе", 3.5m, new[] { "горячее" }),
    new("Круассан", 2.25m, Array.Empty<string>()),
});

// Интерполяция проверяется компилятором, но шаблон «зашит» в код.
// Хранимые отдельно шаблоны — Scriban, Handlebars.NET: поля по имени, через рефлексию
// (Razor же компилирует шаблон в C#).
var sb = new StringBuilder();
sb.AppendLine($"Заказ №{order.Id} для {order.Customer.ToUpper()}");
foreach (var (it, i) in order.Items.Select((it, i) => (it, i)))
{
    sb.Append($"{i + 1}. {it.Name} — {it.Price:F2}");
    if (it.Tags.Length > 0) sb.Append($" [{string.Join(", ", it.Tags)}]");
    sb.AppendLine();
}
Console.Write(sb);

// В Razor то же самое выглядело бы так (и экранировало бы HTML по умолчанию):
// @foreach (var it in Model.Items) { <li>@it.Name — @it.Price.ToString("F2")</li> }

record Item(string Name, decimal Price, string[] Tags);
record Order(int Id, string Customer, List<Item> Items);
