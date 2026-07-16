//go:build linux

package ebpf

import (
	"encoding/binary"
	"net"
	"testing"
)

// The instruction encoding is protocol-frozen against the kernel verifier —
// guard the bytes so a refactor can't silently corrupt the program.
func TestProgramEncoding(t *testing.T) {
	p := program(net.ParseIP("127.77.99.88"))
	if len(p) != 8*8 {
		t.Fatalf("program = %d bytes, want %d", len(p), 8*8)
	}
	// insn 0: LDX_W r2, [r1+4]
	if p[0] != 0x61 || p[1] != 0x12 || binary.LittleEndian.Uint16(p[2:]) != 4 {
		t.Errorf("insn0 (load user_ip4) wrong: % x", p[0:8])
	}
	// insn 5: STX_W [r1+4], r2
	s := p[5*8 : 6*8]
	if s[0] != 0x63 || s[1] != 0x21 || binary.LittleEndian.Uint16(s[2:]) != 4 {
		t.Errorf("insn5 (store user_ip4) wrong: % x", s)
	}
	// insn 4: MOV r2, ip — immediate must equal inet_addr("127.77.99.88")
	mov := p[4*8 : 5*8]
	want := binary.LittleEndian.Uint32(net.ParseIP("127.77.99.88").To4())
	if got := binary.LittleEndian.Uint32(mov[4:]); got != want {
		t.Errorf("insn4 immediate = %#x, want %#x", got, want)
	}
	// insn 7: EXIT
	if p[7*8] != 0x95 {
		t.Errorf("insn7 not EXIT: %#x", p[7*8])
	}
}

func TestProgram6Encoding(t *testing.T) {
	p := program6(net.ParseIP("127.77.99.88"))
	if len(p) != 16*8 {
		t.Fatalf("program6 = %d bytes, want %d", len(p), 16*8)
	}
	// insn 11: STX_W [r1+16], r4 — writes ip6[2] (the ::ffff word)
	s := p[11*8 : 12*8]
	if s[0] != 0x63 || binary.LittleEndian.Uint16(s[2:]) != 16 {
		t.Errorf("insn11 (store ip6[2]) wrong: % x", s)
	}
	// insn 10: MOV r4, 0xffff0000
	if got := binary.LittleEndian.Uint32(p[10*8+4:]); got != 0xffff0000 {
		t.Errorf("insn10 mapped word = %#x, want 0xffff0000", got)
	}
	// insn 12: MOV r5, v4
	want := binary.LittleEndian.Uint32(net.ParseIP("127.77.99.88").To4())
	if got := binary.LittleEndian.Uint32(p[12*8+4:]); got != want {
		t.Errorf("insn12 v4 = %#x, want %#x", got, want)
	}
}

func TestCgroupPathStable(t *testing.T) {
	a := cgroupPath("127.77.60.193")
	if a != cgroupPath("127.77.60.193") {
		t.Fatal("cgroupPath not deterministic")
	}
	if a == cgroupPath("127.77.40.164") {
		t.Fatal("distinct IPs share a cgroup")
	}
}

// Available must never panic regardless of privilege; on an unprivileged CI
// runner it simply returns false.
func TestAvailableNoPanic(t *testing.T) {
	_ = Available()
}
