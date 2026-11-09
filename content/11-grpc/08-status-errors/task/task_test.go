package task

import (
	"fmt"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func mustStatus(t *testing.T, err error) *status.Status {
	t.Helper()
	st := ToStatus(err)
	if st == nil {
		t.Fatalf("ToStatus(%v) = nil", err)
	}
	return st
}

func TestToStatusCodes(t *testing.T) {
	tests := []struct {
		err  error
		code codes.Code
		msg  string
	}{
		{fmt.Errorf("get ord-1: %w", ErrNotFound), codes.NotFound, "not found"},
		{ErrAlreadyExists, codes.AlreadyExists, "already exists"},
		{fmt.Errorf("save: %w", ErrConflict), codes.Aborted, "conflict"},
		{&ValidationError{}, codes.InvalidArgument, "invalid request"},
		{&PreconditionError{Type: "STATUS"}, codes.FailedPrecondition, "precondition failed"},
		{&UnavailableError{}, codes.Unavailable, "temporarily unavailable"},
	}
	for _, tt := range tests {
		st := mustStatus(t, tt.err)
		if st.Code() != tt.code || st.Message() != tt.msg {
			t.Errorf("ToStatus(%v) = %v %q, want %v %q", tt.err, st.Code(), st.Message(), tt.code, tt.msg)
		}
	}
}

func TestToStatusBadRequest(t *testing.T) {
	st := mustStatus(t, &ValidationError{Violations: []FieldViolation{
		{"customer", "must not be empty"},
		{"items[0].quantity", "must be positive"},
	}})
	d := st.Details()
	if len(d) != 1 {
		t.Fatalf("деталей %d, want 1 (BadRequest)", len(d))
	}
	br, ok := d[0].(*errdetails.BadRequest)
	if !ok {
		t.Fatalf("деталь %T, want *errdetails.BadRequest", d[0])
	}
	fv := br.GetFieldViolations()
	if len(fv) != 2 || fv[0].GetField() != "customer" || fv[1].GetField() != "items[0].quantity" || fv[1].GetDescription() != "must be positive" {
		t.Fatalf("нарушения = %v", fv)
	}
}

func TestToStatusInternalHidesDetails(t *testing.T) {
	st := mustStatus(t, fmt.Errorf("query: dial tcp 10.0.3.7:5432: password authentication failed"))
	if st.Code() != codes.Internal || st.Message() != "internal error" || len(st.Details()) != 0 {
		t.Fatalf("ToStatus = %v %q %v, want Internal \"internal error\" без деталей", st.Code(), st.Message(), st.Details())
	}
}

func TestShouldRetry(t *testing.T) {
	withRetry := func(c codes.Code, d time.Duration) error {
		st, _ := status.New(c, "x").WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(d)})
		return st.Err()
	}
	tests := []struct {
		name  string
		err   error
		retry bool
		after time.Duration
	}{
		{"Unavailable", status.Error(codes.Unavailable, "x"), true, 0},
		{"Unavailable+RetryInfo", withRetry(codes.Unavailable, 300*time.Millisecond), true, 300 * time.Millisecond},
		{"Aborted", status.Error(codes.Aborted, "x"), true, 0},
		{"InvalidArgument", status.Error(codes.InvalidArgument, "x"), false, 0},
		{"NotFound", status.Error(codes.NotFound, "x"), false, 0},
		{"DeadlineExceeded", status.Error(codes.DeadlineExceeded, "x"), false, 0},
	}
	for _, tt := range tests {
		retry, after := ShouldRetry(tt.err)
		if retry != tt.retry || after != tt.after {
			t.Errorf("%s: ShouldRetry = %v, %v; want %v, %v", tt.name, retry, after, tt.retry, tt.after)
		}
	}
}
