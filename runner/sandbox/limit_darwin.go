//go:build darwin

package sandbox

// limitNPROC — RLIMIT_NPROC, которого нет в пакете syscall.
const limitNPROC = 0x7
