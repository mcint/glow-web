package registry_test

import (
	"os"
	"testing"
)

// Rung 7 (D): exactly one hub per state dir, chosen by an flock the kernel
// releases when the holder dies.
func TestHubLock_OneHolder(t *testing.T) {
	st := newState(t)
	l1, ok, err := st.TryHubLock()
	if err != nil || !ok {
		t.Fatalf("first: ok=%v err=%v", ok, err)
	}
	if _, ok, err := st.TryHubLock(); err != nil || ok {
		t.Fatalf("second while held: ok=%v err=%v", ok, err)
	}
	if pid := st.HubLockHolder(); pid != os.Getpid() {
		t.Errorf("holder pid = %d, want %d", pid, os.Getpid())
	}
	l1.Close()
	l2, ok, err := st.TryHubLock()
	if err != nil || !ok {
		t.Fatalf("after release: ok=%v err=%v", ok, err)
	}
	l2.Close()
}
