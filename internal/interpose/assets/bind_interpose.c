/*
 * devhost bind() interposer — the universal transparent-rebinding tier.
 *
 * Loaded into dev servers via DYLD_INSERT_LIBRARIES (macOS, applied by
 * `devhost shim-exec` at the FINAL exec, past every SIP hop) or LD_PRELOAD /
 * /etc/ld.so.preload (Linux). The data path is env-free: this library
 * computes the project IP itself — walk up from cwd to the nearest .devhost
 * marker, md5 the root path (byte0 -> third octet, byte1 % 254 + 1 -> fourth,
 * prefix 127.77), exactly matching internal/addr. Outside a marked tree it
 * is a strict no-op.
 *
 * Only IPv4 binds to INADDR_ANY / INADDR_LOOPBACK are rewritten; explicit
 * addresses and AF_INET6 pass through untouched (v6 wildcard rewriting is a
 * documented gap). md5 here derives an address, not security — embedded
 * (RFC 1321, little-endian hosts) so the library links against libc only.
 */
#define _GNU_SOURCE
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <sys/stat.h>
#include <unistd.h>
#include <limits.h>
#include <string.h>
#include <stdio.h>
#include <stdint.h>
#ifndef __APPLE__
#include <dlfcn.h>
#endif

/* ---- md5 (RFC 1321), address derivation only ---- */
typedef struct {
  uint32_t a, b, c, d;
  uint64_t len;
} md5_t;

static uint32_t rol(uint32_t x, int c) { return (x << c) | (x >> (32 - c)); }

static const uint32_t MD5K[64] = {
    0xd76aa478, 0xe8c7b756, 0x242070db, 0xc1bdceee, 0xf57c0faf, 0x4787c62a,
    0xa8304613, 0xfd469501, 0x698098d8, 0x8b44f7af, 0xffff5bb1, 0x895cd7be,
    0x6b901122, 0xfd987193, 0xa679438e, 0x49b40821, 0xf61e2562, 0xc040b340,
    0x265e5a51, 0xe9b6c7aa, 0xd62f105d, 0x02441453, 0xd8a1e681, 0xe7d3fbc8,
    0x21e1cde6, 0xc33707d6, 0xf4d50d87, 0x455a14ed, 0xa9e3e905, 0xfcefa3f8,
    0x676f02d9, 0x8d2a4c8a, 0xfffa3942, 0x8771f681, 0x6d9d6122, 0xfde5380c,
    0xa4beea44, 0x4bdecfa9, 0xf6bb4b60, 0xbebfbc70, 0x289b7ec6, 0xeaa127fa,
    0xd4ef3085, 0x04881d05, 0xd9d4d039, 0xe6db99e5, 0x1fa27cf8, 0xc4ac5665,
    0xf4292244, 0x432aff97, 0xab9423a7, 0xfc93a039, 0x655b59c3, 0x8f0ccc92,
    0xffeff47d, 0x85845dd1, 0x6fa87e4f, 0xfe2ce6e0, 0xa3014314, 0x4e0811a1,
    0xf7537e82, 0xbd3af235, 0x2ad7d2bb, 0xeb86d391};

static const int MD5R[64] = {7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22,
                             7, 12, 17, 22, 5, 9,  14, 20, 5, 9,  14, 20,
                             5, 9,  14, 20, 5, 9,  14, 20, 4, 11, 16, 23,
                             4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23,
                             6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21,
                             6, 10, 15, 21};

static void md5_block(md5_t *m, const unsigned char *p) {
  uint32_t w[16], a = m->a, b = m->b, c = m->c, d = m->d;
  for (int i = 0; i < 16; i++)
    w[i] = (uint32_t)p[i * 4] | (uint32_t)p[i * 4 + 1] << 8 |
           (uint32_t)p[i * 4 + 2] << 16 | (uint32_t)p[i * 4 + 3] << 24;
  for (int i = 0; i < 64; i++) {
    uint32_t f;
    int g;
    if (i < 16) {
      f = (b & c) | (~b & d);
      g = i;
    } else if (i < 32) {
      f = (d & b) | (~d & c);
      g = (5 * i + 1) % 16;
    } else if (i < 48) {
      f = b ^ c ^ d;
      g = (3 * i + 5) % 16;
    } else {
      f = c ^ (b | ~d);
      g = (7 * i) % 16;
    }
    uint32_t tmp = d;
    d = c;
    c = b;
    b = b + rol(a + f + MD5K[i] + w[g], MD5R[i]);
    a = tmp;
  }
  m->a += a;
  m->b += b;
  m->c += c;
  m->d += d;
}

static void md5(const char *s, unsigned char out[16]) {
  md5_t m = {0x67452301, 0xefcdab89, 0x98badcfe, 0x10325476, 0};
  size_t n = strlen(s);
  m.len = (uint64_t)n * 8;
  while (n >= 64) {
    md5_block(&m, (const unsigned char *)s);
    s += 64;
    n -= 64;
  }
  unsigned char tail[128] = {0};
  memcpy(tail, s, n);
  tail[n] = 0x80;
  size_t blocks = (n + 9 <= 64) ? 1 : 2;
  memcpy(tail + blocks * 64 - 8, &m.len, 8);
  for (size_t i = 0; i < blocks; i++) md5_block(&m, tail + i * 64);
  memcpy(out, &m.a, 4);
  memcpy(out + 4, &m.b, 4);
  memcpy(out + 8, &m.c, 4);
  memcpy(out + 12, &m.d, 4);
}

/* ---- project resolution ---- */
static in_addr_t project_ip; /* 0 = not in a .devhost tree, pass through */

__attribute__((constructor)) static void devhost_resolve(void) {
  char dir[PATH_MAX];
  if (!getcwd(dir, sizeof dir)) return;
  for (;;) {
    char marker[PATH_MAX + 16];
    snprintf(marker, sizeof marker, "%s/.devhost", dir);
    struct stat st;
    if (stat(marker, &st) == 0) {
      unsigned char d[16];
      md5(dir, d);
      char ip[32];
      snprintf(ip, sizeof ip, "127.77.%d.%d", d[0], d[1] % 254 + 1);
      project_ip = inet_addr(ip);
      return;
    }
    char *slash = strrchr(dir, '/');
    if (!slash || slash == dir) return;
    *slash = '\0';
  }
}

/* IPv4: rewrite an ANY/loopback bind to the project IP. Returns 1 if rewritten. */
static int rewrite4(const struct sockaddr *addr, struct sockaddr_in *out) {
  if (addr->sa_family != AF_INET) return 0;
  memcpy(out, addr, sizeof *out);
  if (out->sin_addr.s_addr != htonl(INADDR_ANY) &&
      out->sin_addr.s_addr != htonl(INADDR_LOOPBACK))
    return 0;
  out->sin_addr.s_addr = project_ip;
  return 1;
}

/* IPv6: rewrite an ANY/loopback bind to the IPv4-mapped project address
 * (::ffff:127.77.x.y), which a v4 client reaches at the project IP. Returns
 * 1 if rewritten. */
static int rewrite6(const struct sockaddr *addr, struct sockaddr_in6 *out) {
  if (addr->sa_family != AF_INET6) return 0;
  memcpy(out, addr, sizeof *out);
  const struct in6_addr *a = &out->sin6_addr;
  if (memcmp(a, &in6addr_any, sizeof *a) != 0 &&
      memcmp(a, &in6addr_loopback, sizeof *a) != 0)
    return 0;
  unsigned char *b = (unsigned char *)&out->sin6_addr;
  memset(b, 0, 10);
  b[10] = 0xff;
  b[11] = 0xff;
  memcpy(b + 12, &project_ip, 4); /* project_ip is already network order */
  return 1;
}

/* Shared bind wrapper: rewrite v4/v6 wildcard-or-loopback binds, and for v6
 * clear IPV6_V6ONLY so the IPv4-mapped address is reachable over v4. */
static int devhost_do_bind(
    int (*real)(int, const struct sockaddr *, socklen_t),
    int fd, const struct sockaddr *addr, socklen_t len) {
  if (project_ip && addr) {
    if (addr->sa_family == AF_INET) {
      struct sockaddr_in a;
      if (rewrite4(addr, &a)) return real(fd, (struct sockaddr *)&a, sizeof a);
    } else if (addr->sa_family == AF_INET6) {
      struct sockaddr_in6 a;
      if (rewrite6(addr, &a)) {
        int off = 0;
        setsockopt(fd, IPPROTO_IPV6, IPV6_V6ONLY, &off, sizeof off);
        return real(fd, (struct sockaddr *)&a, sizeof a);
      }
    }
  }
  return real(fd, addr, len);
}

/* ---- interposition ---- */
#ifdef __APPLE__

typedef struct {
  const void *repl;
  const void *orig;
} interpose_t;

static int devhost_bind(int fd, const struct sockaddr *addr, socklen_t len) {
  return devhost_do_bind(bind, fd, addr, len);
}

__attribute__((used)) static const interpose_t interposers[]
    __attribute__((section("__DATA,__interpose"))) = {
        {(const void *)devhost_bind, (const void *)bind},
};

#else /* Linux */

int bind(int fd, const struct sockaddr *addr, socklen_t len) {
  static int (*real_bind)(int, const struct sockaddr *, socklen_t);
  if (!real_bind) real_bind = dlsym(RTLD_NEXT, "bind");
  return devhost_do_bind(real_bind, fd, addr, len);
}

#endif
