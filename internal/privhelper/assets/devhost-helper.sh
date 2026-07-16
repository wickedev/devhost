#!/bin/sh
# devhost privileged helper — the ONLY thing devhost ever runs as root.
#
# Installed root-owned at /usr/local/libexec/devhost-helper with a single
# sudoers line allowing it NOPASSWD. Every argument is validated against a
# fixed pattern below; anything else is refused. Audit this file — it is the
# whole trust surface.
#
#   devhost-helper alias <ip>                add <ip> as an lo0 alias (macOS)
#   devhost-helper hosts <ip> <name> <root>  register "<name>.devhost -> <ip>"
#   devhost-helper resolver <port>           route the .devhost TLD to the
#                                            local responder (macOS)
#   devhost-helper resolver-remove           remove that resolver stub
set -eu
PATH=/usr/bin:/bin:/usr/sbin:/sbin
export LC_ALL=C

refuse() { echo "devhost-helper: refusing $1: $2" >&2; exit 65; }
usage() {
  echo "usage: devhost-helper alias <127.77.x.y> | hosts <127.77.x.y> <name> <project-root>" >&2
  exit 64
}

# devhost project IPs only: 127.77.0-255.1-254
valid_ip() {
  printf '%s' "$1" | grep -Eq \
    '^127\.77\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.(25[0-4]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[1-9])$'
}
# a single DNS label
valid_name() { printf '%s' "$1" | grep -Eq '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'; }
# a TCP/UDP port
valid_port() { printf '%s' "$1" | grep -Eq '^[1-9][0-9]{0,4}$' && [ "$1" -le 65535 ]; }
# absolute path; no control chars, '#', or backslash (they could forge lines)
valid_root() { printf '%s' "$1" | grep -Eq '^/[^[:cntrl:]#\\]*$'; }

cmd=${1-}
[ $# -ge 1 ] && shift || usage

# grep validates per-line, so an embedded newline could smuggle a second,
# unvalidated line past every pattern below — refuse it outright, everywhere
NL='
'
for a in "$@"; do
  case "$a" in *"$NL"*) refuse argument "embedded newline" ;; esac
done

case "$cmd" in
alias)
  [ $# -eq 1 ] || usage
  valid_ip "$1" || refuse ip "$1"
  [ "$(uname)" = Darwin ] || exit 0 # linux routes all of 127/8 natively
  exec /sbin/ifconfig lo0 alias "$1" up
  ;;
hosts)
  [ $# -eq 3 ] || usage
  valid_ip "$1" || refuse ip "$1"
  valid_name "$2" || refuse name "$2"
  valid_root "$3" || refuse root "$3"
  tmp=$(mktemp /etc/hosts.devhost.XXXXXX)
  # drop stale devhost-managed lines for this project root, keep all else;
  # the tag is matched as an exact line suffix via env (never interpolated)
  DEVHOST_TAG="# devhost:$3" awk '
    { t = ENVIRON["DEVHOST_TAG"] }
    substr($0, length($0) - length(t) + 1) == t { next }
    { print }
  ' /etc/hosts >"$tmp"
  printf '%s %s.devhost # devhost:%s\n' "$1" "$2" "$3" >>"$tmp"
  chmod 644 "$tmp"
  chown 0:0 "$tmp" 2>/dev/null || chown root:wheel "$tmp"
  mv "$tmp" /etc/hosts
  ;;
resolver)
  [ $# -eq 1 ] || usage
  valid_port "$1" || refuse port "$1"
  [ "$(uname)" = Darwin ] || exit 0 # /etc/resolver is a macOS mechanism
  mkdir -p /etc/resolver
  printf 'nameserver 127.0.0.1\nport %s\n' "$1" >/etc/resolver/devhost
  chmod 644 /etc/resolver/devhost
  ;;
resolver-remove)
  [ $# -eq 0 ] || usage
  rm -f /etc/resolver/devhost
  ;;
*)
  usage
  ;;
esac
