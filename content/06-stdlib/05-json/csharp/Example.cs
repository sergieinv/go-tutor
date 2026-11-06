using System;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Text.Unicode;

var u = new User { Id = 42, Name = "Анна", Password = "secret" };
// Атрибуты вместо struct tags; null-поля пропускаем глобально или атрибутом.
// По умолчанию не-ASCII экранируется ("\u0410…"); Go пишет UTF-8 как есть.
var utf8 = new JsonSerializerOptions { Encoder = JavaScriptEncoder.Create(UnicodeRanges.All) };
Console.WriteLine(JsonSerializer.Serialize(u, utf8));
// {"id":"42","name":"Анна"}

// Строгий режим — аналог Decoder.DisallowUnknownFields
var strict = new JsonSerializerOptions
{
    UnmappedMemberHandling = JsonUnmappedMemberHandling.Disallow,
    PropertyNameCaseInsensitive = true, // в Go разбор без учёта регистра — по умолчанию
};
try { JsonSerializer.Deserialize<User>("""{"name":"Вера","age":30}""", strict); }
catch (JsonException e) { Console.WriteLine(e.Message); }

// JsonNode вместо map[string]any — но числа здесь не теряют точность
var node = System.Text.Json.Nodes.JsonNode.Parse("""{"n": 9007199254740993}""")!;
Console.WriteLine(node["n"]!.GetValue<long>()); // 9007199254740993

class User
{
    [JsonPropertyName("id")]
    [JsonNumberHandling(JsonNumberHandling.WriteAsString | JsonNumberHandling.AllowReadingFromString)]
    public long Id { get; set; }

    [JsonPropertyName("name")] public string Name { get; set; } = "";

    [JsonPropertyName("email")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public string? Email { get; set; }

    [JsonIgnore] public string Password { get; set; } = "";
}
