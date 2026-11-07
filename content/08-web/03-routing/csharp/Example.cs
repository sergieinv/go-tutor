using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.Mvc;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddControllers();
var app = builder.Build();

// Минимальные API: шаблон и метод — как "GET /{$}" и "GET /books/{id}" в ServeMux.
app.MapGet("/", () => "главная");
// Constraint :int — 404 для /books/abc без кода в хендлере. В Go проверку
// (strconv.Atoi) пишут сами, а PathValue — всегда строка.
app.MapGet("/books/{id:int}", (int id) => $"книга {id}");
app.MapDelete("/books/{id:int}", (int id) => Results.NoContent());
// Литерал специфичнее параметра — как в ServeMux.
app.MapGet("/books/new", () => "форма новой книги");
// Catch-all {**path} ≈ {path...}
app.MapGet("/files/{**path}", (string path) => $"файл {path}");
// Хост в шаблоне ≈ "GET admin.local/{$}"
app.MapGet("/", () => "админка").RequireHost("admin.local");

app.MapControllers();
app.Run();

// Атрибутная маршрутизация контроллеров — то же самое в другой форме.
[ApiController]
[Route("authors")]
public class AuthorsController : ControllerBase
{
    [HttpGet("{id:int:min(1)}")] // в Go: проверка id в хендлере, иначе http.NotFound
    public IActionResult Get(int id) => Ok($"автор {id}");

    [HttpDelete("{id:int:min(1)}")]
    public IActionResult Delete(int id) => NoContent();
    // На PUT /authors/1 маршрутизация сама ответит 405 — как ServeMux с заголовком Allow.
}
