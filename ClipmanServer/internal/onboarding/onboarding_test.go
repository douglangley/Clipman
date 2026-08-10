package onboarding

import (
	"sync"
	"testing"
	"time"
)

func TestSetupLinkConsumptionIsAtomic(t *testing.T) {
	m := &Manager{ConfigPath: t.TempDir() + "/settings.json", Now: func() time.Time { return time.Unix(1_700_000_000, 0) }}
	code, _, err := m.Create(5, 1)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	success := 0
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := m.Lookup(code, true); ok {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("successful downloads=%d", success)
	}
}
func TestWrongCodeDoesNotConsume(t *testing.T) {
	m := &Manager{ConfigPath: t.TempDir() + "/settings.json"}
	code, _, _ := m.Create(5, 2)
	if _, ok := m.Lookup("wrong", true); ok {
		t.Fatal("wrong code accepted")
	}
	state, ok := m.Lookup(code, false)
	if !ok || state.RemainingDownloads != 2 {
		t.Fatalf("state=%+v ok=%v", state, ok)
	}
}
