//go:build linux || darwin

package sandbox

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Launch — подкоманда "launch": выставляет rlimit'ы и заменяет себя программой.
// Вызывается уже от имени uid слота, поэтому лимиты можно только понижать.
func Launch(args []string) error {
	fs := flag.NewFlagSet("launch", flag.ContinueOnError)
	cpu := fs.Uint64("cpu", 0, "RLIMIT_CPU, секунды")
	data := fs.Uint64("data", 0, "RLIMIT_DATA, байты")
	fsize := fs.Uint64("fsize", 0, "RLIMIT_FSIZE, байты")
	nofile := fs.Uint64("nofile", 0, "RLIMIT_NOFILE")
	nproc := fs.Uint64("nproc", 0, "RLIMIT_NPROC")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("launch: не указана программа")
	}
	limits := []struct {
		res int
		val uint64
	}{
		{syscall.RLIMIT_CPU, *cpu},
		// Память — RLIMIT_DATA, а не RLIMIT_AS: см. комментарий в exec.
		{syscall.RLIMIT_DATA, *data},
		{syscall.RLIMIT_FSIZE, *fsize},
		{syscall.RLIMIT_NOFILE, *nofile},
		{limitNPROC, *nproc},
	}
	for _, l := range limits {
		if l.val == 0 {
			continue
		}
		hard := l.val
		if l.res == syscall.RLIMIT_CPU {
			// Запас в секунду: сначала SIGXCPU, по которому видно, что кончилось время,
			// и только потом SIGKILL от ядра.
			hard++
		}
		if err := syscall.Setrlimit(l.res, &syscall.Rlimit{Cur: l.val, Max: hard}); err != nil {
			return fmt.Errorf("setrlimit %d: %w", l.res, err)
		}
	}
	prog, err := exec.LookPath(fs.Arg(0))
	if err != nil {
		return err
	}
	return syscall.Exec(prog, fs.Args(), os.Environ())
}

// KillAll — подкоманда "killall": от имени uid слота убивает все его процессы.
// Повторяет kill(-1), пока процессы не кончатся: fork-бомба успевает
// породить новых, пока убиваются старые.
func KillAll() error {
	for range 50 {
		err := syscall.Kill(-1, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return nil // убивать некого
		}
		if err != nil {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}
