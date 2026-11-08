using System.Security.Claims;
using Microsoft.AspNetCore.Authorization;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddAuthentication().AddCookie();
builder.Services.AddAuthorization(o =>
    o.AddPolicy("Admin", p => p.RequireRole("admin"))); // право на функцию — политика
builder.Services.AddSingleton<IAuthorizationHandler, NoteOwnerHandler>();
var app = builder.Build();
app.UseAuthentication();
app.UseAuthorization();

var notes = new Dictionary<int, Note>
{
    [1] = new(1, "alice", "пин-код от сейфа: 4721"),
    [2] = new(2, "bob", "купить молоко"),
};

// RequireAuthorization() = [Authorize]: «кто-то вошёл» — как authn-middleware в Go.
// УЯЗВИМО: о владельце заметки атрибут ничего не знает — любой вошедший читает чужое.
app.MapGet("/v1/notes/{id:int}", (int id) =>
    notes.TryGetValue(id, out var n) ? Results.Ok(n.Text) : Results.NotFound())
    .RequireAuthorization();

// Ресурсная авторизация: объект известен только в обработчике,
// поэтому и в ASP.NET проверка — явный вызов, как в Go.
app.MapGet("/v2/notes/{id:int}", async (int id, ClaimsPrincipal user, IAuthorizationService auth) =>
{
    if (!notes.TryGetValue(id, out var n))
        return Results.NotFound();
    var result = await auth.AuthorizeAsync(user, n, new OwnerRequirement());
    return result.Succeeded ? Results.Ok(n.Text) : Results.NotFound(); // чужое = не найдено
}).RequireAuthorization();

app.MapGet("/admin/notes", () => notes.Values).RequireAuthorization("Admin");

app.Run();

record Note(int Id, string Owner, string Text);

class OwnerRequirement : IAuthorizationRequirement { }

class NoteOwnerHandler : AuthorizationHandler<OwnerRequirement, Note>
{
    protected override Task HandleRequirementAsync(
        AuthorizationHandlerContext ctx, OwnerRequirement req, Note note)
    {
        if (ctx.User.Identity?.Name == note.Owner)
            ctx.Succeed(req);
        return Task.CompletedTask;
    }
}
