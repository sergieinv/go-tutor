package task

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestLintContract(t *testing.T) {
	// Порядок — по Element (байтовое сравнение строк), затем по Rule.
	want := []Issue{
		{RuleFieldLowerSnakeCase, "shop.lint.v1.Order.Line.Quantity"},
		{RuleEnumZeroValueSuffix, "shop.lint.v1.Order.STATUS_NEW"},
		{RuleFieldLowerSnakeCase, "shop.lint.v1.Order.cashOnDelivery"},
		{RuleFieldLowerSnakeCase, "shop.lint.v1.Order.labelMap"},
		{RuleRPCRequestStandardName, "shop.lint.v1.OrderService.CancelOrder"},
		{RuleRPCResponseStandardName, "shop.lint.v1.OrderService.GetOrder"},
		{RuleFieldLowerSnakeCase, "shop.lint.v1.OrderServiceListOrdersRequest.pageToken"},
	}
	got := Lint(File_lintme_proto)
	if !slices.Equal(got, want) {
		t.Fatalf("Lint =\n%v\nwant\n%v", got, want)
	}
}

func TestLintCleanFile(t *testing.T) {
	// В timestamp.proto все имена правильные, сервисов и enum нет.
	if got := Lint(timestamppb.File_google_protobuf_timestamp_proto); len(got) != 0 {
		t.Fatalf("Lint(timestamp.proto) = %v, want пусто", got)
	}
}
