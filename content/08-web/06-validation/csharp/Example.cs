using System.ComponentModel.DataAnnotations;
using Microsoft.AspNetCore.Mvc;

var builder = WebApplication.CreateBuilder(args);
builder.Services.AddControllers();
var app = builder.Build();
app.MapControllers();
app.Run();

// Правила — атрибуты; проверяет их конвейер MVC до вызова action.
// В Go это метод Validate() error, который хендлер вызывает сам.
public class CreateUser : IValidatableObject
{
    [Required, StringLength(50, MinimumLength = 2)] // Length — UTF-16, не байты
    public string Name { get; set; } = "";

    [Required, EmailAddress]
    public string Email { get; set; } = "";

    [Url] // проверяет лишь префикс http://, https:// или ftp:// — строже url.Parse, но не валидатор URL
    public string? Website { get; set; }

    // Перекрёстные правила — как обычный код в Validate() в Go.
    // MVC вызывает этот метод, только если атрибуты свойств уже прошли.
    public IEnumerable<ValidationResult> Validate(ValidationContext ctx)
    {
        if (Website is not null && !Website.StartsWith("https://", StringComparison.OrdinalIgnoreCase))
            yield return new ValidationResult("нужен https", [nameof(Website)]);
    }
}

[ApiController]
[Route("users")]
public class UsersController : ControllerBase
{
    // [ApiController] сам вернёт 400 с ValidationProblemDetails —
    // и для битого JSON, и для неверных полей. В Go: 400 и 422 вручную.
    [HttpPost]
    public IActionResult Create(CreateUser input)
    {
        // Нормализация после проверки — частая ошибка (здесь — намеренно); в Go её делают до Validate.
        input.Email = input.Email.Trim().ToLowerInvariant();
        if (!ModelState.IsValid) // сюда дойдём, только если отключить авто-400
            return UnprocessableEntity(ModelState);
        return Ok($"создан {input.Name} <{input.Email}>");
    }
}
