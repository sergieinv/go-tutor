//go:build unix

package sandbox

import (
	"os/exec"
	"syscall"
)

// sysProcAttr ставит процесс в собственную группу и, если задан uid, меняет его.
func sysProcAttr(uid int) *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{Setpgid: true}
	if uid >= 0 {
		attr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(uid), NoSetGroups: true}
	}
	return attr
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
