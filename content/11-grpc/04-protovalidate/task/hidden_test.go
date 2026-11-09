package task

import (
	"context"
	"errors"
	"testing"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHiddenStableOrder(t *testing.T) {
	c, _ := start(t)
	req := &AddProductRequest{
		Sku: "bad sku", Title: "", PriceCents: 100, SalePriceCents: 200,
		Tags: []string{"ok", "", "x", "", "a", "b"},
	}
	for range 5 {
		_, err := c.AddProduct(context.Background(), req)
		fv := badRequest(t, err)
		type key struct{ field, reason string }
		want := []key{
			{"", "product.sale_below_price"},
			{"sku", "string.pattern"},
			{"tags", "repeated.max_items"},
			{"tags[1]", "string.min_len"},
			{"tags[3]", "string.min_len"},
			{"title", "string.min_len"},
		}
		if len(fv) != len(want) {
			t.Fatalf("violations = %v, want %v", fv, want)
		}
		for i, w := range want {
			if fv[i].GetField() != w.field || fv[i].GetReason() != w.reason || fv[i].GetDescription() == "" {
				t.Fatalf("violation[%d] = %v, want %v (всё: %v)", i, fv[i], w, fv)
			}
		}
	}
}

func TestHiddenBrokenRuleIsInternal(t *testing.T) {
	c, impl := start(t)
	_, err := c.Audit(context.Background(), &AuditRequest{Sku: "MUG-1"})
	st := status.Convert(err)
	if st.Code() != codes.Internal || st.Message() != "internal error" || len(st.Details()) != 0 {
		t.Fatalf("status = %v %q %v, want Internal \"internal error\" без деталей", st.Code(), st.Message(), st.Details())
	}
	if impl.calls.Load() != 0 {
		t.Fatal("обработчик вызван при ошибке правил")
	}
}

func TestHiddenNonProtoAndPassThrough(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	ic := UnaryValidator(v)
	info := &grpc.UnaryServerInfo{FullMethod: "/x/Y"}

	called := false
	resp, err := ic(context.Background(), "не proto", info, func(ctx context.Context, req any) (any, error) {
		called = true
		return "ответ", nil
	})
	if !called || resp != "ответ" || err != nil {
		t.Fatalf("не-proto запрос: called=%v resp=%v err=%v", called, resp, err)
	}

	// Ответ и ошибка обработчика возвращаются без изменений.
	handlerErr := status.Error(codes.NotFound, "no such product")
	resp, err = ic(context.Background(), &RenameRequest{Sku: "MUG-1", Title: "T"}, info,
		func(ctx context.Context, req any) (any, error) { return nil, handlerErr })
	if resp != nil || !errors.Is(err, handlerErr) {
		t.Fatalf("ошибка обработчика изменена: %v, %v", resp, err)
	}
	want := &RenameResponse{}
	resp, err = ic(context.Background(), &RenameRequest{Sku: "MUG-1", Title: "T"}, info,
		func(ctx context.Context, req any) (any, error) { return want, nil })
	if resp != want || err != nil {
		t.Fatalf("ответ обработчика изменён: %v, %v", resp, err)
	}
}

func TestHiddenSortByField(t *testing.T) {
	// Нарушения правил полей без правила сообщения: только два поля, по пути.
	c, _ := start(t)
	_, err := c.AddProduct(context.Background(), &AddProductRequest{
		Sku: "MUG-1", Title: "T", PriceCents: 0, SalePriceCents: -1,
	})
	fv := badRequest(t, err)
	got := []string{}
	for _, f := range fv {
		got = append(got, f.GetField()+"|"+f.GetReason())
	}
	want := []string{"price_cents|int64.gt", "sale_price_cents|int64.gte"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("violations = %v, want %v", got, want)
	}
}
