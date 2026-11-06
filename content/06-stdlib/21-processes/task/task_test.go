package task

import (
	"context"
	"testing"
)

func TestRunOutput(t *testing.T) {
	got, err := Run(context.Background(), "", "/bin/sh", "-c", "echo out; echo err >&2")
	want := Result{Stdout: "out\n", Stderr: "err\n"}
	if err != nil || got != want {
		t.Errorf("Run = %+v, %v; want %+v, nil", got, err, want)
	}
}

func TestRunExitCode(t *testing.T) {
	got, err := Run(context.Background(), "", "/bin/sh", "-c", "echo fail >&2; exit 3")
	want := Result{Stderr: "fail\n", ExitCode: 3}
	if err != nil || got != want {
		t.Errorf("Run = %+v, %v; want %+v, nil — ненулевой код не ошибка Run", got, err, want)
	}
}

func TestRunStdin(t *testing.T) {
	got, err := Run(context.Background(), "gopher\n", "/bin/sh", "-c", `read name; echo "hi, $name"`)
	if err != nil || got.Stdout != "hi, gopher\n" {
		t.Errorf("Run = %+v, %v; want Stdout %q", got, err, "hi, gopher\n")
	}
}
