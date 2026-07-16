//go:build linux

// Package ebpf is the Linux kernel-level rebinding tier. It attaches a
// CGROUP_INET4_BIND program to a per-project cgroup that rewrites bind() from
// 0.0.0.0/127.0.0.1 to the project IP inside the kernel — so it catches
// everything the LD_PRELOAD interposer can't, including static Go binaries
// that issue raw syscalls past libc. Needs CAP_BPF + CAP_NET_ADMIN (root) and
// a writable cgroup2; it's the opt-in endgame tier, not the default path.
//
// Pure syscalls, no cgo, no deps. The instruction encoding and the required
// attr size (144) were verified against the kernel's own headers.
package ebpf

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	cmdProgLoad   = 5
	cmdProgAttach = 8

	progTypeCgroupSockAddr = 18
	attachTypeInet4Bind    = 8

	attrSize = 144 // sizeof(union bpf_attr) — smaller truncates expected_attach_type

	cgroupRoot = "/sys/fs/cgroup"
	cgroupBase = cgroupRoot + "/devhost"
)

func sysBPFNum() uintptr {
	if runtime.GOARCH == "amd64" {
		return 321
	}
	return 280 // arm64
}

func bpf(cmd int, attr []byte) (int, error) {
	r, _, e := syscall.Syscall(sysBPFNum(), uintptr(cmd), uintptr(unsafe.Pointer(&attr[0])), attrSize)
	if e != 0 {
		return 0, e
	}
	return int(r), nil
}

func insn(code byte, dst, src byte, off int16, imm int32) []byte {
	b := make([]byte, 8)
	b[0] = code
	b[1] = (src << 4) | (dst & 0xf)
	binary.LittleEndian.PutUint16(b[2:], uint16(off))
	binary.LittleEndian.PutUint32(b[4:], uint32(imm))
	return b
}

// program builds a bind4 rewrite that only touches wildcard/loopback binds,
// leaving explicit addresses alone — mirroring the interposer's policy.
func program(ip net.IP) []byte {
	const (
		LDX_W     = 0x61 // r = *(u32*)(src+off)
		STX_W     = 0x63 // *(u32*)(dst+off) = src
		MOV64_IMM = 0xb7
		JEQ_K     = 0x15
		JA        = 0x05
		EXIT      = 0x95
	)
	ipImm := int32(binary.LittleEndian.Uint32(ip.To4()))
	const loopback = int32(0x0100007f) // 127.0.0.1 in network byte order
	// user_ip4 is at offset 4 in struct bpf_sock_addr.
	var p []byte
	p = append(p, insn(LDX_W, 2, 1, 4, 0)...)         // 0: r2 = ctx->user_ip4
	p = append(p, insn(JEQ_K, 2, 0, 2, 0)...)         // 1: if r2==0        -> insn4
	p = append(p, insn(JEQ_K, 2, 0, 1, loopback)...)  // 2: if r2==127.0.0.1-> insn4
	p = append(p, insn(JA, 0, 0, 2, 0)...)            // 3: else            -> insn6
	p = append(p, insn(MOV64_IMM, 2, 0, 0, ipImm)...) // 4: r2 = project ip
	p = append(p, insn(STX_W, 1, 2, 4, 0)...)         // 5: ctx->user_ip4 = r2
	p = append(p, insn(MOV64_IMM, 0, 0, 0, 1)...)     // 6: r0 = 1 (allow)
	p = append(p, insn(EXIT, 0, 0, 0, 0)...)          // 7: exit
	return p
}

func loadProgram(ip net.IP) (int, error) {
	prog := program(ip)
	license := append([]byte("GPL"), 0)
	log := make([]byte, 8192)

	attr := make([]byte, attrSize)
	binary.LittleEndian.PutUint32(attr[0:], progTypeCgroupSockAddr)
	binary.LittleEndian.PutUint32(attr[4:], uint32(len(prog)/8))
	binary.LittleEndian.PutUint64(attr[8:], uint64(uintptr(unsafe.Pointer(&prog[0]))))
	binary.LittleEndian.PutUint64(attr[16:], uint64(uintptr(unsafe.Pointer(&license[0]))))
	binary.LittleEndian.PutUint32(attr[24:], 1)
	binary.LittleEndian.PutUint32(attr[28:], uint32(len(log)))
	binary.LittleEndian.PutUint64(attr[32:], uint64(uintptr(unsafe.Pointer(&log[0]))))
	binary.LittleEndian.PutUint32(attr[68:], attachTypeInet4Bind)

	fd, err := bpf(cmdProgLoad, attr)
	if err != nil {
		return 0, fmt.Errorf("BPF_PROG_LOAD: %w", err)
	}
	runtime.KeepAlive(prog)
	runtime.KeepAlive(license)
	runtime.KeepAlive(log)
	return fd, nil
}

func attach(progFd, cgroupFd int) error {
	attr := make([]byte, attrSize)
	binary.LittleEndian.PutUint32(attr[0:], uint32(cgroupFd)) // target_fd
	binary.LittleEndian.PutUint32(attr[4:], uint32(progFd))   // attach_bpf_fd
	binary.LittleEndian.PutUint32(attr[8:], attachTypeInet4Bind)
	binary.LittleEndian.PutUint32(attr[12:], 1) // BPF_F_ALLOW_OVERRIDE
	_, err := bpf(cmdProgAttach, attr)
	return err
}

// cgroupPath is the per-project cgroup for an IP (stable, collision-free).
func cgroupPath(ip string) string {
	return filepath.Join(cgroupBase, "ip-"+ip)
}

// Available reports whether this process can load and attach BPF programs
// (needs privilege and a writable cgroup2). Probes by loading a throwaway
// program and freeing it.
func Available() bool {
	if _, err := os.Stat(cgroupRoot + "/cgroup.controllers"); err != nil {
		return false
	}
	fd, err := loadProgram(net.IPv4(127, 77, 0, 1))
	if err != nil {
		return false
	}
	syscall.Close(fd)
	return true
}

// Activate ensures the bind4 rewrite for ip is attached to the project cgroup
// and returns that cgroup's path. Idempotent: re-attaching over an existing
// program replaces it.
func Activate(ip string) (string, error) {
	path := cgroupPath(ip)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("create cgroup %s: %w", path, err)
	}
	progFd, err := loadProgram(net.ParseIP(ip))
	if err != nil {
		return "", err
	}
	defer syscall.Close(progFd)

	cg, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer cg.Close()
	if err := attach(progFd, int(cg.Fd())); err != nil {
		return "", fmt.Errorf("BPF_PROG_ATTACH: %w", err)
	}
	return path, nil
}

// JoinCgroup moves pid into the project cgroup so the attached program governs
// its binds (and its children's).
func JoinCgroup(cgroupDir string, pid int) error {
	return os.WriteFile(filepath.Join(cgroupDir, "cgroup.procs"),
		[]byte(fmt.Sprint(pid)), 0o644)
}
