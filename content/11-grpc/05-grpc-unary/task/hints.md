## Подсказка 1

Состояние сервиса — заказы в порядке создания (срез) и индекс по id (map), всё под одним `sync.Mutex`: обработчики gRPC вызываются конкурентно, по горутине на вызов. Номер заказа — длина среза плюс один, вычисленная под тем же мьютексом, что и добавление: тогда номера идут без пропусков, а неудачные запросы их не тратят, потому что проверка идёт до блокировки.

## Подсказка 2

Опции сервера передаются в `grpc.NewServer`: `grpc.MaxRecvMsgSize(64 << 10)`. Регистрация — сгенерированная функция `RegisterOrderServiceServer(srv, s)`. `CancelOrder` писать не нужно: встроенная `UnimplementedOrderServiceServer` уже отвечает `codes.Unimplemented`. Ошибки — `status.Error(codes.InvalidArgument, ...)` и `status.Errorf(codes.NotFound, ...)`.

## Подсказка 3

```go
s.mu.Lock()
defer s.mu.Unlock()
o := &Order{
	Id:         fmt.Sprintf("ord-%d", len(s.orders)+1),
	Customer:   req.GetCustomer(),
	Items:      req.GetItems(),
	TotalCents: total, // посчитан до блокировки
}
s.orders = append(s.orders, o)
s.byID[o.Id] = o
return &CreateOrderResponse{Order: proto.Clone(o).(*Order)}, nil
```

В `ListOrders` пройдите по срезу и отберите заказы покупателя (или все, если фильтр пуст); пустой результат — пустой ответ, а не ошибка.
