// Package listen turns a listen spec into a net.Listener.
//
// Specs:
//
//	HOST:PORT        TCP (bare form, what --addr has always meant)
//	tcp:HOST:PORT    TCP, explicit
//	unix:PATH        unix-domain socket at PATH, mode 0600
//
// A unix socket path that already exists is replaced only when it is a
// socket nobody answers on (left behind by a killed process). A live
// socket is "in use"; anything that is not a socket is never deleted.
package listen

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// MaxSockPath is the longest unix socket path accepted: macOS's sun_path is
// 104 bytes including the trailing NUL (Linux allows 108).
const MaxSockPath = 103

// Parse splits a spec into a net.Listen network and address.
func Parse(spec string) (network, address string, err error) {
	switch {
	case strings.HasPrefix(spec, "unix:"):
		network, address = "unix", strings.TrimPrefix(spec, "unix:")
	case strings.HasPrefix(spec, "tcp:"):
		network, address = "tcp", strings.TrimPrefix(spec, "tcp:")
	default:
		// "word:rest" with another ':' in rest is a scheme we don't know
		// ("udp:1.2.3.4:5"); "localhost:8080" stays a bare TCP address.
		if w, rest, ok := strings.Cut(spec, ":"); ok && isWord(w) && strings.Contains(rest, ":") {
			return "", "", fmt.Errorf("listen %q: unknown scheme %q (want tcp: or unix:)", spec, w)
		}
		network, address = "tcp", spec
	}
	if address == "" {
		return "", "", fmt.Errorf("listen %q: empty address", spec)
	}
	return network, address, nil
}

func isWord(s string) bool {
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return s != ""
}

// Listen parses spec and binds it.
func Listen(spec string) (net.Listener, error) {
	network, address, err := Parse(spec)
	if err != nil {
		return nil, err
	}
	if network == "tcp" {
		return net.Listen("tcp", address)
	}
	if len(address) > MaxSockPath {
		return nil, fmt.Errorf("listen unix:%s: socket path too long (%d bytes; the macOS limit is %d). Use a shorter path or GLOW_WEB_STATE_DIR", address, len(address), MaxSockPath)
	}
	if err := clearStale(address); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", address)
	if err != nil {
		return nil, err
	}
	// Chmod after bind leaves a brief window; callers that care put the
	// socket in a 0700 dir (the hub's run/ dir does), which closes it.
	if err := os.Chmod(address, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// Alive reports whether something accepts connections on the unix socket.
func Alive(path string) bool {
	c, err := net.DialTimeout("unix", path, 250*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func clearStale(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("listen unix:%s: path exists and is not a socket; refusing to remove it", path)
	}
	if Alive(path) {
		return fmt.Errorf("listen unix:%s: socket in use by a live process", path)
	}
	return os.Remove(path)
}
