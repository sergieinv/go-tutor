using Grpc.Core;
using Grpc.Core.Interceptors;
using Grpc.Net.Client;

// ASP.NET Core gRPC (пакет Grpc.AspNetCore): interceptors регистрируются
// в DI, порядок — порядок добавления, первый — внешний. Как в Go:
// ChainUnaryInterceptor(logging, recovery, auth).
var builder = WebApplication.CreateBuilder(args);
builder.Services.AddGrpc(o =>
{
    o.Interceptors.Add<LoggingInterceptor>();
    o.Interceptors.Add<AuthInterceptor>();
    // Recovery не нужен: исключение в обработчике не роняет процесс,
    // клиент получает StatusCode.Unknown "Exception was thrown by handler."
    // (с EnableDetailedErrors = true — ещё и текст исключения, только для отладки).
    o.EnableDetailedErrors = false;
    o.MaxReceiveMessageSize = 1 << 20; // как grpc.MaxRecvMsgSize, по умолчанию 4 МБ
});
var app = builder.Build();
// app.MapGrpcService<OrderService>(); — класс, сгенерированный Grpc.Tools из .proto
app.Run();

// Interceptor — базовый класс с виртуальными методами под каждый вид вызова:
// UnaryServerHandler, ServerStreamingServerHandler, ... и клиентские AsyncUnaryCall и т.д.
// В Go это отдельные типы функций: UnaryServerInterceptor, StreamServerInterceptor.
public class LoggingInterceptor(ILogger<LoggingInterceptor> log) : Interceptor
{
    public override async Task<TResponse> UnaryServerHandler<TRequest, TResponse>(
        TRequest request, ServerCallContext context, UnaryServerMethod<TRequest, TResponse> continuation)
    {
        var rid = context.RequestHeaders.GetValue("x-request-id");
        try
        {
            var resp = await continuation(request, context); // как handler(ctx, req) в Go
            log.LogInformation("{Method} rid={Rid} code=OK", context.Method, rid);
            return resp;
        }
        catch (RpcException e)
        {
            // Ошибка gRPC — исключение RpcException, а не второе возвращаемое значение.
            log.LogInformation("{Method} rid={Rid} code={Code}", context.Method, rid, e.StatusCode);
            throw;
        }
    }
}

public class AuthInterceptor : Interceptor
{
    public override Task<TResponse> UnaryServerHandler<TRequest, TResponse>(
        TRequest request, ServerCallContext context, UnaryServerMethod<TRequest, TResponse> continuation)
    {
        var header = context.RequestHeaders.GetValue("authorization");
        var token = header is not null && header.StartsWith("Bearer ") ? header["Bearer ".Length..] : "";
        if (!System.Security.Cryptography.CryptographicOperations.FixedTimeEquals(
                System.Text.Encoding.UTF8.GetBytes(token), "s3cret"u8))
        {
            throw new RpcException(new Status(StatusCode.Unauthenticated, "invalid token"));
        }
        // Пользователя кладут в context.UserState (словарь на вызов) — аналог context.WithValue.
        context.UserState["user"] = "alice";
        return continuation(request, context);
    }
    // На практике аутентификацию в ASP.NET Core делают не interceptor'ом, а
    // middleware AddAuthentication/AddAuthorization и атрибутом [Authorize] на сервисе.
}

// Клиентский interceptor: AsyncUnaryCall оборачивает вызов, как UnaryClientInterceptor.
public class RequestIdInterceptor : Interceptor
{
    private int _n;

    public override AsyncUnaryCall<TResponse> AsyncUnaryCall<TRequest, TResponse>(
        TRequest request, ClientInterceptorContext<TRequest, TResponse> context,
        AsyncUnaryCallContinuation<TRequest, TResponse> continuation)
    {
        var headers = context.Options.Headers ?? new Metadata();
        headers.Add("x-request-id", $"req-{Interlocked.Increment(ref _n)}");
        var ctx = new ClientInterceptorContext<TRequest, TResponse>(
            context.Method, context.Host, context.Options.WithHeaders(headers));
        return continuation(request, ctx);
    }

    // Подключение: порядок в Intercept(a, b, c) — a получает управление первым.
    public static CallInvoker Wrap(GrpcChannel channel) => channel.Intercept(new RequestIdInterceptor());
}
