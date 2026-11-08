using System.Net.Security;
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;
using System.Text;

// AES-GCM: AesGcm ≈ cipher.NewGCM. Nonce и тег .NET отдаёт отдельными буферами —
// склеить nonce + шифртекст + тег, как seal в Go, нужно самому.
byte[] key = RandomNumberGenerator.GetBytes(32);
byte[] plaintext = Encoding.UTF8.GetBytes("card=4111111111111111");
byte[] aad = Encoding.UTF8.GetBytes("user:42");

using var aes = new AesGcm(key, tagSizeInBytes: 16);
byte[] nonce = RandomNumberGenerator.GetBytes(AesGcm.NonceByteSizes.MaxSize); // 12 байт, каждый раз новый
byte[] ciphertext = new byte[plaintext.Length];
byte[] tag = new byte[16];
aes.Encrypt(nonce, plaintext, ciphertext, tag, aad);

byte[] opened = new byte[ciphertext.Length];
aes.Decrypt(nonce, ciphertext, tag, opened, aad); // при подмене — AuthenticationTagMismatchException, в Go — error
Console.WriteLine(Encoding.UTF8.GetString(opened));

// Ключи под разные цели: HKDF.DeriveKey ≈ hkdf.Key.
byte[] macKey = HKDF.DeriveKey(HashAlgorithmName.SHA256, key, 32, info: Encoding.UTF8.GetBytes("links/hmac"));
byte[] mac = HMACSHA256.HashData(macKey, Encoding.UTF8.GetBytes("/files/report.pdf"));
Console.WriteLine(CryptographicOperations.FixedTimeEquals(mac, mac)); // ≈ hmac.Equal

// TLS. Аналог InsecureSkipVerify — и такая же дыра:
//   handler.ServerCertificateCustomValidationCallback =
//       HttpClientHandler.DangerousAcceptAnyServerCertificateValidator;
// Правильно — доверять своему CA, как RootCAs в Go: проверка имени и срока остаётся.
static HttpClient TrustingClient(X509Certificate2 internalCa)
{
    var policy = new X509ChainPolicy { TrustMode = X509ChainTrustMode.CustomRootTrust };
    policy.CustomTrustStore.Add(internalCa);
    var handler = new SocketsHttpHandler
    {
        SslOptions = new SslClientAuthenticationOptions { CertificateChainPolicy = policy },
    };
    return new HttpClient(handler) { Timeout = TimeSpan.FromSeconds(5) };
}
_ = (Func<X509Certificate2, HttpClient>)TrustingClient; // в приложении — при регистрации HttpClient
