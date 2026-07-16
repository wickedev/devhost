// devhost: transparently rebind Node servers to the per-project loopback IP
// ($DEVHOST). Loaded via NODE_OPTIONS="--require ..." set by the devhost
// shim/exec — projects need zero code or script changes.
//
// Only rewrites binds that target "any"/loopback (0.0.0.0, ::, localhost,
// 127.0.0.1, ::1, or no host). Explicit hosts and unix-socket paths pass
// through untouched.
'use strict';
const DEV = process.env.DEVHOST;
if (DEV) {
  const net = require('net');
  const LOCAL = new Set(['0.0.0.0', '::', 'localhost', '127.0.0.1', '::1', '', undefined, null]);
  const origListen = net.Server.prototype.listen;
  net.Server.prototype.listen = function (...args) {
    try {
      if (typeof args[0] === 'object' && args[0] !== null) {
        const o = args[0];
        if (o.port !== undefined && o.path === undefined && LOCAL.has(o.host)) {
          args[0] = Object.assign({}, o, { host: DEV });
        }
      } else if (
        typeof args[0] === 'number' ||
        (typeof args[0] === 'string' && /^\d+$/.test(args[0]))
      ) {
        if (typeof args[1] === 'string') {
          if (LOCAL.has(args[1])) args[1] = DEV;
        } else {
          args.splice(1, 0, DEV); // listen(port, [backlog/cb...]) -> insert host
        }
      }
    } catch (_) {
      // never break the app over rewriting — fall through to original args
    }
    return origListen.apply(this, args);
  };
}
