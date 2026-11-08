using System.Text.Encodings.Web;
using System.Text.Json;

var app = WebApplication.Create(args);

// В Razor то же делает движок: @Model.Name экранирует HTML,
// @Html.Raw(x) — аналог template.HTML, отключает экранирование.
// Без Razor (минимальный API, строки) кодировщик под контекст
// выбираете сами — html/template делает этот выбор за вас.
app.MapGet("/profile", (HttpContext ctx) =>
{
    var name = "Tom & \"Jerry\" <script>";
    var html =
        $"<p>{HtmlEncoder.Default.Encode(name)}</p>" +
        $"<a href=\"/u?name={UrlEncoder.Default.Encode(name)}\">профиль</a>" +
        // JavaScriptEncoder.Default по умолчанию экранирует < > & ' —
        // </script> из данных не закроет тег, как и в html/template.
        $"<script>let user = {JsonSerializer.Serialize(name)};</script>";

    // Заголовки в ASP.NET Core тоже не ставятся сами (кроме Content-Type).
    // С этим CSP встроенный <script> выше браузер не выполнит:
    // свой скрипт помечают nonce или выносят в файл.
    ctx.Response.Headers.XContentTypeOptions = "nosniff";
    ctx.Response.Headers.ContentSecurityPolicy =
        "default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'";
    return Results.Content(html, "text/html; charset=utf-8");
});

app.Run();
