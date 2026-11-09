using Grpc.Core;
using Shop.V1; // сгенерировано Grpc.Tools из того же orders.proto с google.api.http

// Microsoft.AspNetCore.Grpc.JsonTranscoding — аналог grpc-gateway, но внутри
// ASP.NET Core: те же аннотации google.api.http, REST и gRPC на одном порту,
// без отдельного прокси и без сгенерированного кода маршрутов (*.pb.gw.go).
var builder = WebApplication.CreateBuilder(args);
builder.Services.AddGrpc().AddJsonTranscoding(o =>
{
    // Как JSONPb{MarshalOptions{UseProtoNames, EmitUnpopulated}} в gateway.
    o.JsonSettings.WriteIndented = false;
    o.JsonSettings.IgnoreDefaultValues = false;
});
// OpenAPI: пакет Microsoft.AspNetCore.Grpc.Swagger (в Go — protoc-gen-openapiv2).
var app = builder.Build();
app.MapGrpcService<OrderService>(); // один вызов — и gRPC, и REST-маршруты
app.Run();

public class OrderService : Shop.V1.OrderService.OrderServiceBase
{
    private readonly List<Order> _orders = [new() { Id = 1, CustomerId = "c1", TotalCents = 1500, Status = OrderStatus.New }];

    public override Task<GetOrderResponse> GetOrder(GetOrderRequest request, ServerCallContext context)
    {
        var order = _orders.FirstOrDefault(o => o.Id == request.Id)
            // RpcException с NotFound превратится в HTTP 404 — то же соответствие кодов,
            // что runtime.HTTPStatusFromCode. Тело ошибки — google.rpc.Status в JSON.
            ?? throw new RpcException(new Status(StatusCode.NotFound, $"order {request.Id} not found"));
        return Task.FromResult(new GetOrderResponse { Order = order });
    }

    public override Task<ListOrdersResponse> ListOrders(ListOrdersRequest request, ServerCallContext context)
    {
        // Оба маршрута (get и additional_bindings) приходят сюда; customer_id —
        // из пути или из query, обработчику всё равно.
        var resp = new ListOrdersResponse();
        resp.Orders.AddRange(_orders.Where(o => request.CustomerId == "" || o.CustomerId == request.CustomerId));
        // Заголовки HTTP-запроса видны как metadata, без настройки matcher'а.
        _ = context.RequestHeaders.GetValue("x-request-id");
        return Task.FromResult(resp);
    }
}
