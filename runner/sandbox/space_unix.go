//go:build unix

package sandbox

import "syscall"

// freeBytes — свободное место в файловой системе каталога. Нужно, чтобы
// отличить «код не собирается» от «в общем tmpfs рабочих каталогов не
// осталось места»: место мог занять другой запуск.
func freeBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
