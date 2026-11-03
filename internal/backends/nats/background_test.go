package natsbackend

import "testing"

func backgroundRunning() bool {
	bgMu.Lock()
	defer bgMu.Unlock()
	return bgStop != nil
}

func TestBackgroundStartsOnPoolUseAndStopsOnShutdown(t *testing.T) {
	Shutdown()
	Shutdown() // safe when nothing is running
	if backgroundRunning() {
		t.Fatal("cleanup loops running after shutdown")
	}

	_, _ = DefaultPool.GetConnection("no-such-target")
	if !backgroundRunning() {
		t.Fatal("pool use did not start the cleanup loops")
	}
	ensureBackground() // second call must not start a second pair
	Shutdown()
	if backgroundRunning() {
		t.Fatal("shutdown left the cleanup loops running")
	}
}
