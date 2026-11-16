package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Contract is the run-record contract major this binary speaks
// (specs/unified-listener.md §4.V). The hub routes records with the same
// major whatever their glow-web version. Adding fields is a minor change;
// removing or renaming one bumps this.
const Contract = 1

// Record is run/<name>.json: one open session. It is live exactly while
// its writer holds an flock on the file, so a crash releases it without
// cleanup.
type Record struct {
	Contract int       `json:"contract"`
	Name     string    `json:"name"`
	Root     string    `json:"root"`
	PID      int       `json:"pid"`
	Version  string    `json:"version"`
	Sock     string    `json:"sock,omitempty"` // proxy mode: socket file name in run/; empty = lease
	Started  time.Time `json:"started"`
}

// Usable reports whether this binary understands the record's contract.
func (r Record) Usable() bool { return r.Contract == Contract }

// Mode is "proxy" when the session serves its own socket, else "lease".
func (r Record) Mode() string {
	if r.Sock != "" {
		return "proxy"
	}
	return "lease"
}

// SockPath is the absolute socket path of a proxy record ("" for a lease).
func (s State) SockPath(r Record) string {
	if r.Sock == "" {
		return ""
	}
	return filepath.Join(s.RunDir(), filepath.Base(r.Sock))
}

// SockName is the socket file name for name in run/: "<name>.sock" when
// the full path fits macOS's 104-byte sun_path, else "<fnv32a(name)>.sock".
// The record's Sock field carries whichever was used.
func (s State) SockName(name string) string {
	plain := name + ".sock"
	if len(filepath.Join(s.RunDir(), plain)) <= 103 {
		return plain
	}
	h := fnv.New32a()
	h.Write([]byte(name))
	return fmt.Sprintf("%08x.sock", h.Sum32())
}

// Lease is a held registration. Close releases it and removes the record
// (and the socket, for proxy mode).
type Lease struct {
	f    *os.File
	path string
	sock string
}

// Register writes run/<rec.Name>.json and holds its lock until Close. It
// fills in Contract, PID and Started. It is refused when a live record or
// a pin of a *different* root holds the name. The error names the holder
// and suggests --name.
func (s State) Register(rec Record) (*Lease, error) {
	if err := ValidName(rec.Name); err != nil {
		return nil, err
	}
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	pins, _ := s.Pins()
	for _, p := range pins {
		if p.Name == rec.Name && p.Root != rec.Root {
			return nil, fmt.Errorf("name %q is published for %s; pick another with --name", rec.Name, p.Root)
		}
	}
	rec.Contract, rec.PID, rec.Started = Contract, os.Getpid(), time.Now().UTC().Truncate(time.Second)
	body, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return nil, err
	}

	// Write and lock under a hidden temp name, then link() into place:
	// a record is never visible half-written or unlocked, and link fails
	// rather than overwrite a concurrent registrant.
	tmp := filepath.Join(s.RunDir(), fmt.Sprintf(".%s.%d.tmp", rec.Name, rec.PID))
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		f.Close()
		return nil, err
	}
	final := filepath.Join(s.RunDir(), rec.Name+".json")
	for attempt := 0; ; attempt++ {
		err = os.Link(tmp, final)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) || attempt > 0 {
			f.Close()
			return nil, err
		}
		holder, live, err := s.probe(final)
		if err != nil {
			f.Close()
			return nil, err
		}
		if live {
			f.Close()
			return nil, fmt.Errorf("name %q is open by pid %d for %s; pick another with --name", rec.Name, holder.PID, holder.Root)
		}
		// probe pruned the stale record; try once more.
	}
	return &Lease{f: f, path: final, sock: s.SockPath(rec)}, nil
}

// Close releases the lease and removes its files.
func (l *Lease) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	os.Remove(l.path)
	if l.sock != "" {
		os.Remove(l.sock)
	}
	err := l.f.Close() // closing the fd drops the flock
	l.f = nil
	return err
}

// abandon drops the lock but leaves the files, as a crash would.
func (l *Lease) abandon() {
	l.f.Close()
	l.f = nil
}

// probe reads a record and reports whether its holder is alive. A stale
// record with a contract we understand is pruned, together with its
// socket. One we don't understand is left in place: a hub never deletes
// what it cannot read.
func (s State) probe(path string) (Record, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return Record{}, false, err
	}
	defer f.Close()
	var rec Record
	_ = json.NewDecoder(f).Decode(&rec)
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return rec, true, nil
	}
	if err != nil {
		return rec, false, err
	}
	if rec.Usable() {
		os.Remove(path)
		if sp := s.SockPath(rec); sp != "" {
			os.Remove(sp)
		}
	}
	return rec, false, nil // our lock is dropped when f closes
}

// Records lists live run records, sorted by name, plus any record with
// a contract this binary does not speak, so callers can surface it.
// Stale records are pruned as a side effect, as are temp files left by a
// crash during Register.
func (s State) Records() []Record {
	ents, err := os.ReadDir(s.RunDir())
	if err != nil {
		return nil
	}
	var out []Record
	for _, e := range ents {
		n := e.Name()
		p := filepath.Join(s.RunDir(), n)
		if strings.HasPrefix(n, ".") && strings.HasSuffix(n, ".tmp") {
			if f, err := os.Open(p); err == nil {
				if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
					os.Remove(p)
				}
				f.Close()
			}
			continue
		}
		if strings.HasPrefix(n, ".") || !strings.HasSuffix(n, ".json") {
			continue
		}
		rec, live, err := s.probe(p)
		if err != nil {
			continue
		}
		if live || !rec.Usable() {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
