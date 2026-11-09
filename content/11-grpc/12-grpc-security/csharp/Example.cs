// Пакеты: Grpc.AspNetCore (сервер) и Grpc.Net.Client (клиент).
// Проект — Microsoft.NET.Sdk.Web.
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;
using System.Text;
using Grpc.AspNetCore.Server;
using Grpc.Core;
using Grpc.Core.Interceptors;
using Grpc.Net.Client;
using Microsoft.AspNetCore.Server.Kestrel.Https;

var builder = WebApplication.CreateBuilder(args);

// mTLS настраивается в Kestrel, а не в gRPC: обязательный сертификат клиента
// и своя проверка цепочки по внутреннему CA (в Go — tls.Config с ClientCAs
// и RequireAndVerifyClientCert, переданный в credentials.NewTLS).
var internalCa = X509CertificateLoader.LoadCertificateFromFile("ca.crt");
builder.WebHost.ConfigureKestrel(k => k.ConfigureHttpsDefaults(https =>
{
    https.ClientCertificateMode = ClientCertificateMode.RequireCertificate;
    https.ClientCertificateValidation = (cert, _, _) =>
    {
        using var chain = new X509Chain();
        chain.ChainPolicy.TrustMode = X509ChainTrustMode.CustomRootTrust;
        chain.ChainPolicy.CustomTrustStore.Add(internalCa);
        chain.ChainPolicy.RevocationMode = X509RevocationMode.NoCheck;
        return chain.Build(cert);
    };
}));

// Interceptor регистрируется для всех сервисов — как grpc.UnaryInterceptor.
// Идиоматичнее для ASP.NET Core — AddAuthentication().AddCertificate()/
// AddJwtBearer() и [Authorize(Roles = "admin")] на методах сервиса: конвейер
// сделает то же, что в Go пишут руками.
builder.Services.AddGrpc(o => o.Interceptors.Add<AuthInterceptor>());
builder.Services.AddSingleton(new TokenStore(new()
{
    [Environment.GetEnvironmentVariable("BILLING_TOKEN") ?? ""] = ("billing", ["reader"]),
}));

var app = builder.Build();
// app.MapGrpcService<OrdersService>(); — сервис из сгенерированного кода
if (args.Contains("--serve"))
    app.Run();

// Клиент: сертификат — в HttpClientHandler, токен — CallCredentials.
// GrpcChannel откажется слать CallCredentials по http:// (без TLS), пока явно
// не включить UnsafeUseInsecureChannelCallCredentials — аналог
// RequireTransportSecurity в Go.
var handler = new HttpClientHandler();
handler.ClientCertificates.Add(X509CertificateLoader.LoadPkcs12FromFile("billing.pfx", null));
var token = Environment.GetEnvironmentVariable("BILLING_TOKEN") ?? "";
var callCreds = CallCredentials.FromInterceptor((_, metadata) =>
{
    metadata.Add("Authorization", $"Bearer {token}");
    return Task.CompletedTask;
});
using var channel = GrpcChannel.ForAddress("https://orders.internal:5001", new GrpcChannelOptions
{
    HttpHandler = handler,
    Credentials = ChannelCredentials.Create(new SslCredentials(), callCreds),
    MaxReceiveMessageSize = 1 << 20, // в Go — grpc.MaxCallRecvMsgSize / MaxRecvMsgSize
});

public record TokenStore(Dictionary<string, (string Service, string[] Roles)> Tokens);

public class AuthInterceptor(TokenStore store) : Interceptor
{
    static readonly Dictionary<string, string[]> MethodRoles = new()
    {
        ["/orders.v1.OrderService/GetOrder"] = ["reader", "admin"],
        ["/orders.v1.OrderService/CancelOrder"] = ["admin"],
    };

    public override Task<TResponse> UnaryServerHandler<TRequest, TResponse>(
        TRequest request, ServerCallContext context, UnaryServerMethod<TRequest, TResponse> continuation)
    {
        // Личность из сертификата: Kestrel уже проверил цепочку. В Go —
        // peer.FromContext → credentials.TLSInfo → VerifiedChains[0][0].
        var cert = context.GetHttpContext().Connection.ClientCertificate;
        var service = cert?.GetNameInfo(X509NameType.SimpleName, false);
        if (string.IsNullOrEmpty(service))
            throw new RpcException(new Status(StatusCode.Unauthenticated, "unauthenticated"));

        var header = context.RequestHeaders.Where(h => h.Key == "authorization").ToList();
        var presented = header.Count == 1 && header[0].Value.StartsWith("Bearer ")
            ? Encoding.UTF8.GetBytes(header[0].Value["Bearer ".Length..])
            : [];
        // Сравнение за постоянное время — как subtle.ConstantTimeCompare.
        var grant = store.Tokens
            .Where(t => CryptographicOperations.FixedTimeEquals(presented, Encoding.UTF8.GetBytes(t.Key)))
            .Select(t => t.Value).FirstOrDefault();
        if (presented.Length == 0 || grant.Service != service)
            throw new RpcException(new Status(StatusCode.Unauthenticated, "unauthenticated"));

        if (!MethodRoles.TryGetValue(context.Method, out var roles) || !roles.Intersect(grant.Roles).Any())
            throw new RpcException(new Status(StatusCode.PermissionDenied, "permission denied"));

        return continuation(request, context);
    }
}
