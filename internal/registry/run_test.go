package registry_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcint/glow-web/internal/registry"
)

func TestRegister_LiveWhileHeld_GoneAfterClose(t *testing.T) {
	st := newState(t)
	root := t.TempDir()
	lease, err := st.Register(registry.Record{Name: "proj", Root: root, Version: "v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	recs := st.Records()
	if len(recs) != 1 {
		t.Fatalf("records = %+v", recs)
	}
	r := recs[0]
	if r.Name != "proj" || r.Root != root || r.PID != os.Getpid() || r.Contract != registry.Contract || r.Version != "v1.2.3" || r.Started.IsZero() {
		t.Errorf("record = %+v", r)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if recs := st.Records(); len(recs) != 0 {
		t.Errorf("after Close records = %+v", recs)
	}
	if _, err := os.Stat(filepath.Join(st.RunDir(), "proj.json")); !os.IsNotExist(err) {
		t.Errorf("record file left behind: %v", err)
	}
}

func TestRegister_CrashedHolderIsPruned(t *testing.T) {
	st := newState(t)
	lease, err := st.Register(registry.Record{Name: "proj", Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	registry.AbandonForTest(lease) // drop the lock, leave the file: a crash
	if recs := st.Records(); len(recs) != 0 {
		t.Fatalf("crashed record still listed: %+v", recs)
	}
	if _, err := os.Stat(filepath.Join(st.RunDir(), "proj.json")); !os.IsNotExist(err) {
		t.Errorf("stale record not pruned: %v", err)
	}
	// And the name is free again.
	l2, err := st.Register(registry.Record{Name: "proj", Root: t.TempDir()})
	if err != nil {
		t.Fatalf("re-register after crash: %v", err)
	}
	l2.Close()
}

func TestRegister_LiveNameRefusedNamingHolder(t *testing.T) {
	st := newState(t)
	a := t.TempDir()
	l, err := st.Register(registry.Record{Name: "proj", Root: a})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, err = st.Register(registry.Record{Name: "proj", Root: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("err = %v; want holder %s and a --name hint", err, a)
	}
}

func TestRegister_PinOfSameNameDifferentRootRefused(t *testing.T) {
	st := newState(t)
	pinned := t.TempDir()
	if _, err := st.Publish(pinned, "proj"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Register(registry.Record{Name: "proj", Root: t.TempDir()}); err == nil {
		t.Fatal("record shadowing a pin of a different root: want error")
	}
	l, err := st.Register(registry.Record{Name: "proj", Root: pinned})
	if err != nil {
		t.Fatalf("same root as the pin should be allowed: %v", err)
	}
	l.Close()
}

func TestRecords_UnknownContractLeftAlone(t *testing.T) {
	st := newState(t)
	p := filepath.Join(st.RunDir(), "future.json")
	body := `{"contract": 99, "name": "future", "root": "/x", "pid": 1}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	recs := st.Records()
	if len(recs) != 1 || recs[0].Usable() {
		t.Fatalf("records = %+v; want one unusable", recs)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("a hub must not delete a record it cannot read: %v", err)
	}
}

func TestRegister_RejectsBadName(t *testing.T) {
	st := newState(t)
	if _, err := st.Register(registry.Record{Name: "_x", Root: t.TempDir()}); err == nil {
		t.Error("reserved name accepted")
	}
}
