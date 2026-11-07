using System.Text.Json;
using System.Text.Json.Serialization;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.Mvc;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddControllers()
    // Строгий разбор ≈ Decoder.DisallowUnknownFields()
    .AddJsonOptions(o => o.JsonSerializerOptions.UnmappedMemberHandling = JsonUnmappedMemberHandling.Disallow);
builder.Services.AddProblemDetails(); // ошибки в формате RFC 9457, как writeProblem
builder.Services.AddSingleton<TaskStore>();
var app = builder.Build();
app.MapControllers();
app.Run();

// Без [Required]: он отверг бы пустой и пробельный title кодом 400, а нам нужен 422, как в Go.
// string? — иначе при Nullable enable отсутствующее поле тоже дало бы автоматический 400.
public record CreateTask(string? Title);
public record TaskItem(int Id, string Title, bool Done);

[ApiController] // model binding + автоматический 400 с ValidationProblemDetails
[Route("tasks")]
public class TasksController(TaskStore store) : ControllerBase
{
    [HttpPost]
    [Consumes("application/json")]     // иначе 415 — в Go проверка mime.ParseMediaType
    [RequestSizeLimit(1 << 20)]         // иначе 413 — в Go http.MaxBytesReader
    public IActionResult Create(CreateTask input)
    {
        // Сюда попадаем, только если тело разобрано и ModelState валиден.
        if (string.IsNullOrWhiteSpace(input.Title))
            return Problem(statusCode: 422, detail: "title пустой"); // ≈ writeProblem(w, 422, ...)
        var t = store.Add(input.Title);
        return CreatedAtAction(nameof(Get), new { id = t.Id }, t); // 201 + Location
    }

    [HttpGet("{id:int}")]
    public ActionResult<TaskItem> Get(int id) =>
        store.Find(id) is { } t ? t : Problem(statusCode: 404, detail: $"нет задачи {id}");
}

public class TaskStore
{
    private readonly Dictionary<int, TaskItem> _items = new();
    private readonly object _lock = new(); // запросы параллельны, как горутины в Go

    internal TaskItem Add(string title)
    {
        lock (_lock)
        {
            var t = new TaskItem(_items.Count + 1, title, false);
            _items[t.Id] = t;
            return t;
        }
    }

    internal TaskItem? Find(int id)
    {
        lock (_lock) return _items.GetValueOrDefault(id);
    }
}
