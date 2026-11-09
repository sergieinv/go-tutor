using FluentValidation;
using Google.Protobuf.WellKnownTypes;
using Google.Rpc;
using Grpc.Core;
using Grpc.Core.Interceptors;

// Правила в C#: FluentValidation-валидатор на тип запроса. Их видит только
// этот сервис; в Go те же правила лежат в orders.proto (buf.validate),
// и их одинаково применяют сервер, клиенты на других языках и фронтенд.
var builder = WebApplication.CreateBuilder(args);
builder.Services.AddSingleton<IValidator<CreateOrderRequest>, CreateOrderValidator>();
// Interceptor регистрируется на все сервисы, как grpc.UnaryInterceptor в Go.
builder.Services.AddGrpc(o => o.Interceptors.Add<ValidationInterceptor>());
var app = builder.Build();
// app.MapGrpcService<OrderService>(); — сервис из сгенерированной базы
app.Run();

// В настоящем проекте это класс, сгенерированный Grpc.Tools из .proto.
public sealed class CreateOrderRequest
{
    public string CustomerEmail { get; set; } = "";
    public List<Item> Items { get; } = new();
    public uint DiscountPercent { get; set; }
    public string Coupon { get; set; } = "";
}

public sealed record Item(string Sku, uint Quantity, long PriceCents);

public sealed class CreateOrderValidator : AbstractValidator<CreateOrderRequest>
{
    public CreateOrderValidator()
    {
        // ↔ [(buf.validate.field).string.email = true]
        RuleFor(x => x.CustomerEmail).EmailAddress().WithErrorCode("string.email");
        // ↔ repeated = {min_items: 1, max_items: 20}
        RuleFor(x => x.Items).NotEmpty().Must(i => i.Count <= 20);
        RuleForEach(x => x.Items).ChildRules(item =>
        {
            item.RuleFor(i => i.Sku).Matches("^[A-Z0-9-]+$").Length(3, 20);
            item.RuleFor(i => i.Quantity).InclusiveBetween(1u, 100u);
            // ↔ field-level CEL: this % 100 == 0
            item.RuleFor(i => i.PriceCents).Must(p => p % 100 == 0).WithMessage("price must be in whole rubles");
        });
        // ↔ (buf.validate.message).cel — правило между полями.
        RuleFor(x => x).Must(x => x.DiscountPercent <= 10 || x.Coupon != "")
            .WithMessage("discount above 10% requires a coupon");
    }
}

// Проверка один раз для всех методов — как interceptor в Go.
public sealed class ValidationInterceptor(IServiceProvider services) : Interceptor
{
    public override async Task<TResponse> UnaryServerHandler<TRequest, TResponse>(
        TRequest request, ServerCallContext context, UnaryServerMethod<TRequest, TResponse> continuation)
    {
        if (services.GetService(typeof(IValidator<TRequest>)) is IValidator<TRequest> validator)
        {
            var result = await validator.ValidateAsync(request, context.CancellationToken);
            if (!result.IsValid)
            {
                // google.rpc.BadRequest в деталях; ToRpcException — из пакета Grpc.StatusProto.
                var badRequest = new BadRequest();
                foreach (var e in result.Errors.OrderBy(e => e.PropertyName, StringComparer.Ordinal))
                {
                    badRequest.FieldViolations.Add(new BadRequest.Types.FieldViolation
                    {
                        // Поле Reason есть в новых google.rpc.BadRequest; в Google.Api.CommonProtos,
                        // которые тянет Grpc.StatusProto 2.71, его ещё нет.
                        Field = e.PropertyName, Description = e.ErrorMessage,
                    });
                }
                var status = new Google.Rpc.Status
                {
                    Code = (int)Code.InvalidArgument,
                    Message = "invalid request",
                    Details = { Any.Pack(badRequest) },
                };
                // В Go — return nil, st.Err(): ошибка значением, а не исключением.
                throw status.ToRpcException();
            }
        }
        return await continuation(request, context);
    }
}
