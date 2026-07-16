//go:build darwin

package daemon

import (
	"encoding/binary"
	"errors"
	"syscall"
	"unsafe"
)

// In-process caller lookup via the proc_info(2) syscall — no fork+exec, so it
// replaces the ~100ms lsof round-trip on the localhost mirror's hot path with
// a sub-millisecond scan. Struct offsets were read from the SDK headers
// (<sys/proc_info.h>); sockets_darwin.go falls back to lsof if this yields
// nothing, so a future layout change degrades speed, not correctness.

const (
	sysProcInfo = 336

	callListPIDs  = 1
	callPIDInfo   = 2
	callPIDFDInfo = 3

	procAllPIDs = 1

	flavListFDs     = 1
	flavFDSocket    = 3
	flavVnodePaths  = 9
	fdTypeSocket    = 2
	sockinfoTCP     = 2
	socketFDInfoLen = 792
	vnodePathLen    = 2352

	// absolute offsets within socket_fdinfo (psi at 24)
	offSoiKind = 256 // int   soi_kind
	offFPort   = 264 // in_sockinfo.insi_fport (network order, first 2 bytes)
	offLPort   = 268 // in_sockinfo.insi_lport
	// within proc_vnodepathinfo
	offCwdPath = 152 // pvi_cdir.vip_path
)

func procInfo(call, pid, flavor int32, arg uint64, buf []byte) (int, error) {
	var p unsafe.Pointer
	if len(buf) > 0 {
		p = unsafe.Pointer(&buf[0])
	}
	r, _, e := syscall.Syscall6(sysProcInfo, uintptr(call), uintptr(pid),
		uintptr(flavor), uintptr(arg), uintptr(p), uintptr(len(buf)))
	if e != 0 {
		return 0, e
	}
	return int(r), nil
}

func listPIDs() ([]int32, error) {
	buf := make([]byte, 4*8192)
	n, err := procInfo(callListPIDs, procAllPIDs, 0, 0, buf)
	if err != nil {
		return nil, err
	}
	pids := make([]int32, 0, n/4)
	for i := 0; i+4 <= n; i += 4 {
		if pid := int32(binary.LittleEndian.Uint32(buf[i:])); pid != 0 {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// pidBySrcPortProc finds the PID whose TCP socket has local port srcPort and
// foreign port dstPort (the client end of a loopback connection).
func pidBySrcPortProc(srcPort, dstPort int) (int, error) {
	pids, err := listPIDs()
	if err != nil {
		return 0, err
	}
	fdbuf := make([]byte, 8*4096)
	si := make([]byte, socketFDInfoLen)
	for _, pid := range pids {
		n, err := procInfo(callPIDInfo, pid, flavListFDs, 0, fdbuf)
		if err != nil {
			continue
		}
		for i := 0; i+8 <= n; i += 8 {
			if binary.LittleEndian.Uint32(fdbuf[i+4:]) != fdTypeSocket {
				continue
			}
			fd := int32(binary.LittleEndian.Uint32(fdbuf[i:]))
			m, err := procInfo(callPIDFDInfo, pid, flavFDSocket, uint64(fd), si)
			if err != nil || m < socketFDInfoLen {
				continue
			}
			if int32(binary.LittleEndian.Uint32(si[offSoiKind:])) != sockinfoTCP {
				continue
			}
			if int(binary.BigEndian.Uint16(si[offLPort:])) == srcPort &&
				int(binary.BigEndian.Uint16(si[offFPort:])) == dstPort {
				return int(pid), nil
			}
		}
	}
	return 0, errors.New("caller pid not found")
}

// cwdOfPidProc returns a process's current working directory.
func cwdOfPidProc(pid int) (string, error) {
	buf := make([]byte, vnodePathLen)
	if _, err := procInfo(callPIDInfo, int32(pid), flavVnodePaths, 0, buf); err != nil {
		return "", err
	}
	path := buf[offCwdPath:]
	end := 0
	for end < len(path) && path[end] != 0 {
		end++
	}
	if end == 0 {
		return "", errors.New("empty cwd")
	}
	return string(path[:end]), nil
}
