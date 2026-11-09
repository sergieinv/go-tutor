## Подсказка 1

`HeaderCarrier` хранит указатель на запись, поэтому методы с получателем-значением могут менять `c.r.Headers`. `Get` — цикл до первого совпадения `h.Key == key`. `Keys` — цикл с `map[string]bool` для уже виденных ключей. `Inject` и `Extract` — одна строка каждая: `p.Inject(ctx, NewHeaderCarrier(r))` и `return p.Extract(ctx, NewHeaderCarrier(r))`.

## Подсказка 2

`Set` проще всего собрать как новый срез: пройдите по заголовкам, чужие ключи копируйте как есть, на первом заголовке с нужным ключом положите `kgo.RecordHeader{Key: key, Value: []byte(value)}` и запомните, что замена сделана, остальные с этим ключом пропускайте. Если замены не было — добавьте заголовок в конец. Именно поэтому повторный `Inject` не плодит второй `traceparent`.

## Подсказка 3

В `Consume`: `ctx = Extract(ctx, p, r)`, затем `ctx, span := tracer.Start(ctx, "process "+r.Topic, trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attribute.String("messaging.system", "kafka"), ...))` и сразу `defer span.End()`. Ошибку `handle` запишите через `span.RecordError(err)` и `span.SetStatus(codes.Error, err.Error())` (пакет `go.opentelemetry.io/otel/codes`) и верните её.
