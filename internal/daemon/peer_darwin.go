//go:build darwin

package daemon

import (
	"errors"
	"net"
	"syscall"
)

// macOS peer-credential getsockopt: level SOL_LOCAL, option LOCAL_PEERPID
// returns the connecting process's PID as a 4-byte int (<sys/un.h>).
const (
	solLocal     = 0     // SOL_LOCAL
	localPeerPID = 0x002 // LOCAL_PEERPID
)

// peerPID returns the PID of the process on the other end of a unix socket.
func peerPID(uc *net.UnixConn) (int, error) {
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var opErr error
	if err := raw.Control(func(fd uintptr) {
		pid, opErr = syscall.GetsockoptInt(int(fd), solLocal, localPeerPID)
	}); err != nil {
		return 0, err
	}
	if opErr != nil {
		return 0, opErr
	}
	if pid <= 0 {
		return 0, errors.New("no peer pid")
	}
	return pid, nil
}
