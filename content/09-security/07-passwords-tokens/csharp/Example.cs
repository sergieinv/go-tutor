using System.Security.Cryptography;
using System.Text;
using Microsoft.AspNetCore.Identity;

// Пароли: PasswordHasher из ASP.NET Core Identity — PBKDF2-HMAC-SHA512, 100 000 итераций,
// соль и параметры внутри строки хеша (как PHC-строка argon2id в Go).
// В стандартной библиотеке Go такого нет: bcrypt и argon2 берут из golang.org/x/crypto.
var hasher = new PasswordHasher<User>();
var alice = new User("alice");
string hash = hasher.HashPassword(alice, "correct horse");
// Success, Failed или SuccessRehashNeeded — «пароль верный, но параметры устарели, перехешируйте».
Console.WriteLine(hasher.VerifyHashedPassword(alice, hash, "correct horse"));

// Токен сессии: RandomNumberGenerator ≈ crypto/rand. System.Random — это math/rand, для секретов не годится.
string token = Convert.ToBase64String(RandomNumberGenerator.GetBytes(16));
// В хранилище — хеш токена, как hashToken в Go.
string stored = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(token)));
Console.WriteLine(stored.Length); // 64

// HMAC-подпись и сравнение за постоянное время: FixedTimeEquals ≈ hmac.Equal / subtle.ConstantTimeCompare.
byte[] key = RandomNumberGenerator.GetBytes(32);
byte[] payload = Encoding.UTF8.GetBytes("alice|1767229200");
byte[] sig = HMACSHA256.HashData(key, payload);
Console.WriteLine(CryptographicOperations.FixedTimeEquals(sig, HMACSHA256.HashData(key, payload))); // True

// JWT в ASP.NET Core проверяет JwtBearer, и почти всё включено по умолчанию:
//   new TokenValidationParameters {
//       ValidateLifetime = true,        // exp; RequireExpirationTime = true — без exp отказ
//       ValidateAudience = true,        // aud; ValidAudience = "api"
//       RequireSignedTokens = true,     // alg "none" не пройдёт
//       ValidAlgorithms = ["HS256"],    // а вот это по умолчанию null — задайте явно
//       IssuerSigningKey = new SymmetricSecurityKey(key),
//   }
// В Go JWT в стандартной библиотеке нет: и в библиотеке, и в своём коде каждый пункт — ваша ответственность.

record User(string Name);
