using System.Security.Claims;
using System.Text.Encodings.Web;
using Microsoft.Extensions.FileProviders;

var builder = WebApplication.CreateBuilder(args);
// Лимит тела по умолчанию ~30 МБ; здесь — как в задаче. В Go: MaxBytesReader.
builder.WebHost.ConfigureKestrel(o => o.Limits.MaxRequestBodySize = 64 * 1024);
builder.Services.AddAuthentication().AddBearerToken();
builder.Services.AddAuthorization();
builder.Services.AddProblemDetails(); // исключения → ProblemDetails без стека
var app = builder.Build();
app.UseExceptionHandler();

var notes = new List<Note> { new(1, "alice", "Покупки", "молоко\n<b>хлеб</b>") };

// RequireAuthorization ≈ [Authorize]: «пользователь вошёл».
// Что заметка его — проверка в коде, как в Go (WHERE owner = ?).
var api = app.MapGroup("/notes").RequireAuthorization();

api.MapGet("/{id:long}", (long id, ClaimsPrincipal user) =>
    notes.FirstOrDefault(n => n.Id == id && n.Owner == user.Identity?.Name) is { } note
        ? Results.Ok(note)
        : Results.NotFound()); // чужая = несуществующая

api.MapGet("/{id:long}/html", (long id, ClaimsPrincipal user) =>
{
    var note = notes.FirstOrDefault(n => n.Id == id && n.Owner == user.Identity?.Name);
    if (note is null) return Results.NotFound();
    // Razor экранировал бы @note.Body сам; вручную — сначала Encode, потом <br>.
    // Html.Raw(note.Body) — то же, что template.HTML(n.Body) в Go: XSS.
    var body = HtmlEncoder.Default.Encode(note.Body).Replace("&#xA;", "<br>");
    return Results.Content($"<h1>{HtmlEncoder.Default.Encode(note.Title)}</h1><div>{body}</div>", "text/html; charset=utf-8");
});

// PhysicalFileProvider отвергает ".." и абсолютные пути, но симлинк наружу
// отдаст; os.Root в Go закрывает и его.
var files = new PhysicalFileProvider(Path.Combine(app.Environment.ContentRootPath, "files"));
app.MapGet("/files", (string name) =>
{
    var info = files.GetFileInfo(name); // "../secret.env" → NotFoundFileInfo
    return info.Exists && !info.IsDirectory
        ? Results.Stream(info.CreateReadStream(), "application/octet-stream")
        : Results.NotFound();
}).RequireAuthorization();

app.Run();

record Note(long Id, string Owner, string Title, string Body);
