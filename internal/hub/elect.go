package hub

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mcint/glow-web/internal/listen"
	"github.com/mcint/glow-web/internal/registry"
)

// Elector makes "first listener wins" work (rung 7, D). Every hub-aware
// process can hold one. Whoever takes the registry's hub lock binds the hub
// listeners (TCP Addr, Host-checked, plus run/hub.sock) and serves; the
// others retry. All routing state is on disk, so a successor needs nothing
// from its predecessor: no fd passing, no re-registration.
type Elector struct {
	State     registry.State
	Addr      string      // TCP listen spec for the hub, e.g. "127.0.0.1:7171"
	AllowHost []string    // extra Host names for the TCP listener
	NewHub    func() *Hub // builds the hub on winning; State/BaseURL are set by the Elector
	// Log, when set, receives one line per election event.
	Log func(string)

	mu     sync.Mutex
	lock   *registry.HubLock
	srvs   []*http.Server
	errc   chan error
	hub    *Hub
	warned bool
}

// TryOnce tries to become the hub. It returns true while this Elector is
// the hub. Losing to another holder is (false, nil). Holding the lock but
// failing to bind (the port is taken by something else) releases the lock
// and returns the bind error.
func (e *Elector) TryOnce() (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lock != nil {
		return true, nil
	}
	lock, ok, err := e.State.TryHubLock()
	if err != nil || !ok {
		return false, err
	}
	tcp, err := listen.Listen(e.Addr)
	if err != nil {
		lock.Close()
		return false, fmt.Errorf("won the hub election but can't bind %s: %w", e.Addr, err)
	}
	// Only the lock holder touches hub.sock, so clearing a stale socket
	// here can't race another hub.
	sock, err := listen.Listen("unix:" + filepath.Join(e.State.RunDir(), "hub.sock"))
	if err != nil {
		tcp.Close()
		lock.Close()
		return false, err
	}
	h := e.NewHub()
	st := e.State
	h.State = &st
	if tcp.Addr().Network() == "tcp" {
		h.BaseURL = "http://" + tcp.Addr().String()
	}
	e.lock, e.hub, e.errc = lock, h, make(chan error, 2)
	public := &http.Server{Handler: HostCheck(h.Handler(), e.AllowHost)}
	private := &http.Server{Handler: h.Handler()}
	e.srvs = []*http.Server{public, private}
	go func() { e.errc <- public.Serve(tcp) }()
	go func() { e.errc <- private.Serve(sock) }()
	e.logf("glow-web: now the hub at %s%s/ (pid %d)", h.BaseURL, h.URLPrefix, os.Getpid())
	return true, nil
}

// Hub returns the hub this Elector serves, or nil when it isn't the hub.
func (e *Elector) Hub() *Hub {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hub
}

// Run calls TryOnce every interval until stop closes. It logs the first
// bind failure only, so a squatted port doesn't spam.
func (e *Elector) Run(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := e.TryOnce(); err != nil && !e.warned {
			e.warned = true
			e.logf("glow-web: %v (will keep trying)", err)
		}
		select {
		case <-stop:
			return
		case err := <-e.errChan():
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				e.logf("glow-web: hub stopped: %v", err)
			}
		case <-t.C:
		}
	}
}

func (e *Elector) errChan() <-chan error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.errc // nil (blocks forever) until this Elector wins
}

// Wait blocks until the hub's listeners stop. Call it after a won TryOnce.
func (e *Elector) Wait() error {
	return <-e.errChan()
}

// Close stops serving and releases the lock, letting another process win.
func (e *Elector) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.srvs {
		s.Close()
	}
	e.srvs, e.hub, e.errc = nil, nil, nil
	if e.lock != nil {
		os.Remove(filepath.Join(e.State.RunDir(), "hub.sock"))
		e.lock.Close()
		e.lock = nil
	}
	return nil
}

func (e *Elector) logf(format string, a ...any) {
	if e.Log != nil {
		e.Log(fmt.Sprintf(format, a...))
	}
}
