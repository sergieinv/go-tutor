using System;
using System.Web;

var u = new Uri("https://anna:secret@Shop.example.com:8443/каталог/a%20b?q=go+lang&tag=x&tag=y#reviews");

// Uri сам приводит схему и хост к нижнему регистру; url.Parse — нет
Console.WriteLine($"{u.Scheme} {u.Host} {u.Port} {u.AbsolutePath} {u.Fragment}");

// Параметры запроса — NameValueCollection; в Go url.Values = map[string][]string
var q = HttpUtility.ParseQueryString(u.Query);
Console.WriteLine($"{q["q"]} {q["tag"]}"); // "go lang x,y"

// UriBuilder ≈ литерал url.URL{...}
var b = new UriBuilder("https", "api.example.com") { Path = "/users/Анна К", Query = "v=1+2" };
Console.WriteLine(b.Uri.AbsoluteUri);

// Экранирование: EscapeDataString ≈ url.PathEscape (пробел → %20)
Console.WriteLine(Uri.EscapeDataString("a b/c&d"));

// Относительная ссылка ≈ base.ResolveReference(ref)
var baseUri = new Uri("https://example.com/docs/guide/intro");
Console.WriteLine(new Uri(baseUri, "../api"));  // https://example.com/docs/api

// Неверный URL — исключение; в Go url.Parse многое принимает без ошибки
Console.WriteLine(Uri.TryCreate("example.com/path", UriKind.Absolute, out _)); // False
