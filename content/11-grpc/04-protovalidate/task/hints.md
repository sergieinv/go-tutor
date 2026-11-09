## Подсказка 1

Interceptor — функция `func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error)`. Сначала `msg, ok := req.(proto.Message)`; не сообщение — `return handler(ctx, req)`. Затем `err := v.Validate(msg)`; `nil` — тоже сразу к обработчику.

## Подсказка 2

Различайте ошибки: `var verr *protovalidate.ValidationError; if !errors.As(err, &verr) { return nil, status.Error(codes.Internal, "internal error") }`. Для `ValidationError` соберите `[]*errdetails.BadRequest_FieldViolation` из `verr.Violations`: путь — `protovalidate.FieldPathString(v.Proto.GetField())`, текст — `v.Proto.GetMessage()`, id правила — `v.Proto.GetRuleId()`.

## Подсказка 3

Сортировка: `slices.SortFunc(out, func(a, b *errdetails.BadRequest_FieldViolation) int { return cmp.Or(cmp.Compare(a.Field, b.Field), cmp.Compare(a.Reason, b.Reason), cmp.Compare(a.Description, b.Description)) })`. Статус с деталями:

```go
st, err := status.New(codes.InvalidArgument, "invalid request").
	WithDetails(&errdetails.BadRequest{FieldViolations: out})
if err != nil {
	return nil, status.Error(codes.Internal, "internal error")
}
return nil, st.Err()
```
