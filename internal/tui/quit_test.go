package tui

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a goroutine-safe writer: the program renders from its own
// goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// TestQuitStopsTheDisplayOnTheFailurePath: when every provider failed, the
// caller returned without ever stopping the display (only Complete did), so
// the spinner kept repainting over the error output and the terminal state
// was never restored. The failure path now calls Quit; it must stop the
// program promptly even though no synthesis or Complete ever happened.
func TestQuitStopsTheDisplayOnTheFailurePath(t *testing.T) {
	p := New(false)
	p.out = &syncBuffer{}
	p.isTTY = true
	p.RegisterProviders([]ProviderInfo{{Name: "grok", DisplayName: "grok"}})
	p.Start()
	p.ProviderStart("grok")
	p.ProviderDone("grok", time.Millisecond, 0, false, errors.New("boom"))

	done := make(chan struct{})
	go func() {
		p.Quit()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Quit did not stop the display program")
	}
}
