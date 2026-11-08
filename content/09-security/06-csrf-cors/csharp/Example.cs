var builder = WebApplication.CreateBuilder(args);

// CSRF: антифорджери-токен — скрытое поле формы + кука, их сверяет
// middleware. В Go 1.25 вместо токенов — CrossOriginProtection
// (проверка Sec-Fetch-Site/Origin), без состояния и без правок в формах.
builder.Services.AddAntiforgery();

// Кука сессии — те же атрибуты, что у http.Cookie в Go.
builder.Services.AddAuthentication().AddCookie(o =>
{
    o.Cookie.HttpOnly = true;
    o.Cookie.SecurePolicy = CookieSecurePolicy.Always;
    o.Cookie.SameSite = SameSiteMode.Lax;
});

// CORS: политика со списком origin. WithOrigins сравнивает точно;
// AllowAnyOrigin() вместе с AllowCredentials() ASP.NET Core запрещает
// при сборке политики. В Go такой страховки нет — заголовки пишете сами.
builder.Services.AddCors(o => o.AddPolicy("app", p => p
    .WithOrigins("https://app.example")
    .AllowCredentials()
    .WithMethods("GET", "POST")
    .WithHeaders("Content-Type")));

var app = builder.Build();
app.UseCors("app");
app.UseAntiforgery(); // проверяет токен у эндпоинтов, принимающих форму

// [FromForm] включает проверку антифорджери-токена для минимального API.
app.MapPost("/transfer", ([Microsoft.AspNetCore.Mvc.FromForm] int amount) =>
    Results.Text($"переведено {amount}"));

app.Run();
