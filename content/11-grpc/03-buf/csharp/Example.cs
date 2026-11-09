using Google.Protobuf.Reflection;
using Shop.V1;

// В .NET генерацию встраивает в сборку Grpc.Tools (внутри — protoc):
//   <PackageReference Include="Grpc.Tools" Version="2.71.0" PrivateAssets="All" />
//   <Protobuf Include="..\proto\shop\v1\orders.proto" ProtoRoot="..\proto"
//             GrpcServices="Server" />
// Линтера и проверки совместимости в этой цепочке нет — их добавляют
// отдельным шагом CI: buf lint и buf breaking работают с тем же .proto.

// Дескриптор файла вкомпилирован в сгенерированный класс OrdersReflection —
// как File_orders_proto в Go. По нему можно пройти так же, как это делает buf.
FileDescriptor fd = OrdersReflection.Descriptor;
Console.WriteLine($"файл: {fd.Name} пакет: {fd.Package}");

foreach (var service in fd.Services)
{
    foreach (var method in service.Methods)
    {
        // Путь gRPC тот же, что в Go: /shop.v1.OrderService/GetOrder.
        Console.WriteLine($"метод /{service.FullName}/{method.Name}({method.InputType.Name}) -> {method.OutputType.Name}");
    }
}

foreach (var message in fd.MessageTypes)
{
    foreach (var field in message.Fields.InFieldNumberOrder())
    {
        // C#-свойство всегда PascalCase (CustomerEmail), а JSON-имя берётся
        // из .proto: у customerEmail и customer_email оно одинаковое.
        Console.WriteLine($"  {message.Name}.{field.Name} #{field.FieldNumber} json={field.JsonName}");
    }
}

// То же правило ENUM_VALUE_PREFIX, что в примере на Go.
foreach (var e in fd.EnumTypes)
{
    string prefix = UpperSnake(e.Name) + "_";
    foreach (var v in e.Values.Where(v => !v.Name.StartsWith(prefix, StringComparison.Ordinal)))
    {
        Console.WriteLine($"ENUM_VALUE_PREFIX {fd.Package}.{v.Name}: нужен префикс {prefix}");
    }
}

static string UpperSnake(string s) =>
    string.Concat(s.Select((c, i) => i > 0 && char.IsUpper(c) ? "_" + c : char.ToUpperInvariant(c).ToString()));
