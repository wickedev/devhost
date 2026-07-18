//go:build linux

package daemon

import (
	"net"
	"syscall"
)

// peerPID returns the PID of the process on the other end of a unix socket,
// via SO_PEERCRED.
func peerPID(uc *net.UnixConn) (int, error) {
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var opErr error
	if err := raw.Control(func(fd uintptr) {
		cred, opErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if opErr != nil {
		return 0, opErr
	}
	return int(cred.Pid), nil
}
