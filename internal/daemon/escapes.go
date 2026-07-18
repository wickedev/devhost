package daemon

// Escape is a listener that bound plain loopback (or the wildcard) from
// inside a .devhost project — a dev server the injection tiers missed, e.g.
// a native binary launched through a SIP-protected hop that stripped the
// interposer. `devhost doctor` reports these so an un-shimmed launcher is a
// named finding instead of a silent isolation miss.
type Escape struct {
	PID     int
	Command string // process name as reported by the OS
	Port    int
	Root    string // the .devhost project root the process ran from
}
