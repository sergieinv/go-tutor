package task

import (
	"errors"
	"reflect"
	"testing"
)

// Профиль в том виде, в каком его печатает
// pprof.Lookup("goroutine").WriteTo(w, 1) (пути укорочены).
const sample = `goroutine profile: total 7
3 @ 0x1007eff78 0x10078b0b0 0x10078ac34 0x100845a34 0x1007f60e4
#	0x100845a33	main.(*server).handle+0x23	/app/main.go:13

2 @ 0x1007eff78 0x1007d03b4 0x1007d0395 0x1007f10a8 0x1007f8e34 0x1008459ec 0x10084599d
# labels: {"job":"x"}
#	0x1007f10a7	internal/sync.runtime_SemacquireMutex+0x27	/go/src/runtime/sema.go:95
#	0x1007f8e33	internal/sync.(*Mutex).lockSlow+0x173		/go/src/internal/sync/mutex.go:149
#	0x1008459eb	internal/sync.(*Mutex).Lock+0x6b		/go/src/internal/sync/mutex.go:70
#	0x10084599c	sync.(*Mutex).Lock+0x1c				/go/src/sync/mutex.go:46
#	0x1008459a0	main.waitLock+0x20				/app/main.go:15
#	0x100845998	main.main.func1+0x18				/app/main.go:26
#	0x10083e747	runtime/pprof.Do+0x77				/go/src/runtime/pprof/runtime.go:57

1 @ 0x1007b56bc 0x1007ef2e4 0x100837534 0x1008371e0 0x100834a44 0x100845928 0x1007be5c0
#	0x100837533	runtime/pprof.writeRuntimeProfile+0xb3	/go/src/runtime/pprof/pprof.go:848
#	0x1008371df	runtime/pprof.writeGoroutine+0x4f	/go/src/runtime/pprof/pprof.go:781
#	0x100834a43	runtime/pprof.(*Profile).WriteTo+0x143	/go/src/runtime/pprof/pprof.go:405
#	0x100845927	main.main+0x277				/app/main.go:30
#	0x1007be5bf	runtime.main+0x37f			/go/src/runtime/proc.go:302

1 @ 0x1007eff78 0x1007d00c0 0x100845a6c 0x1007f60e4
#	0x100845a6b	main.main.func2+0x1b	/app/main.go:28

`

func TestBlockedSample(t *testing.T) {
	got, err := Blocked(sample, "main")
	if err != nil {
		t.Fatalf("Blocked: %v", err)
	}
	want := []Site{
		{"main.(*server).handle", 3},
		{"main.waitLock", 2},
		{"main.main", 1},
		{"main.main.func2", 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Blocked() =\n%v\nwant\n%v", got, want)
	}
}

func TestBlockedOtherPackage(t *testing.T) {
	got, err := Blocked(sample, "sync")
	if err != nil {
		t.Fatalf("Blocked: %v", err)
	}
	want := []Site{{"sync.(*Mutex).Lock", 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Blocked(sync) = %v, want %v", got, want)
	}
}

func TestBlockedNotAProfile(t *testing.T) {
	if _, err := Blocked("hello\n", "main"); !errors.Is(err, ErrFormat) {
		t.Fatalf("err = %v, want ErrFormat", err)
	}
}
