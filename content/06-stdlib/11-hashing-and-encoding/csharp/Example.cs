using System;
using System.IO;
using System.Security.Cryptography;
using System.Text;

var data = Encoding.UTF8.GetBytes("hello, go");

// Хеш целиком
byte[] sum = SHA256.HashData(data);
Console.WriteLine(Convert.ToHexString(sum).ToLowerInvariant()); // hex.EncodeToString

// Потоково — IncrementalHash (в Go hash.Hash — просто io.Writer)
using var inc = IncrementalHash.CreateHash(HashAlgorithmName.SHA256);
inc.AppendData(Encoding.UTF8.GetBytes("hello, "));
inc.AppendData(Encoding.UTF8.GetBytes("go"));
Console.WriteLine(inc.GetHashAndReset().AsSpan().SequenceEqual(sum));

// base64: стандартный; URL-вариант без '=' приходится собирать вручную (до .NET 9)
var raw = new byte[] { 0xfb, 0xff, 0xbf, 0x01 };
var std = Convert.ToBase64String(raw);
Console.WriteLine(std);                                                  // +/+/AQ==
Console.WriteLine(std.TrimEnd('=').Replace('+', '-').Replace('/', '_')); // -_-_AQ

// HMAC и сравнение за постоянное время
var key = Encoding.UTF8.GetBytes("секрет"); // для примера; настоящий ключ — из хранилища секретов
var sig = HMACSHA256.HashData(key, data);
var again = HMACSHA256.HashData(key, data);
Console.WriteLine(CryptographicOperations.FixedTimeEquals(sig, again)); // hmac.Equal
