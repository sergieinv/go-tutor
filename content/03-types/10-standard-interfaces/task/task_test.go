package task

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestCountingWriter(t *testing.T) {
	var buf bytes.Buffer
	cw := &CountingWriter{W: &buf}
	fmt.Fprintf(cw, "one\ntwo\n")
	io.WriteString(cw, "three")
	if buf.String() != "one\ntwo\nthree" {
		t.Errorf("в W записано %q, want %q", buf.String(), "one\ntwo\nthree")
	}
	if cw.Bytes != 13 || cw.Lines != 2 {
		t.Errorf("Bytes, Lines = %d, %d; want 13, 2", cw.Bytes, cw.Lines)
	}
}

func TestUpperReader(t *testing.T) {
	got, err := io.ReadAll(UpperReader{strings.NewReader("Hello, Go 1.25!")})
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "HELLO, GO 1.25!" {
		t.Errorf("прочитано %q, want %q", got, "HELLO, GO 1.25!")
	}
}
