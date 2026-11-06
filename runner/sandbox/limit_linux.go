//go:build linux

package sandbox

// limitNPROC — RLIMIT_NPROC, которого нет в пакете syscall.
const limitNPROC = 0x6
