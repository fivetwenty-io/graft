package graft

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/features"
)

// TestConfigure_CachingFeatureFlipBuildsCache is the F14 regression guard.
// The F2 fix compared e.IsFeatureEnabled(FeatureCaching) against the
// pending flags' state to decide whether to skip the cache rebuild, but
// read the "before" side *after* the option loop ran. WithCaching(enabled)
// mutates its *features.FeatureFlags in place via Set, and when a
// Configure call supplies no WithFeatureFlags option, newOpts.FeatureFlags
// is the *same* pointer as e.Features - so by the time the "before" value
// was read, it already reflected the option's mutation, and the flag flip
// was invisible to the change check. A Configure(WithCaching(true)) call
// on an engine with EnableCache already true (so that field alone doesn't
// trigger a rebuild) but FeatureCaching disabled enabled the flag while
// silently leaving the cache nil.
func TestConfigure_CachingFeatureFlipBuildsCache(t *testing.T) {
	flags := features.DefaultFlags()
	flags.Disable(features.FeatureCaching)

	engine, err := NewEngine(WithCache(true, 1000), WithFeatureFlags(flags))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}

	// Setup precondition: EnableCache is already true and unaffected by
	// what follows, so only the FeatureCaching flag will change - the
	// exact shape that hid the bug from the value-field comparisons.
	if !de.opts.EnableCache {
		t.Fatal("setup: expected EnableCache already true")
	}
	if de.IsFeatureEnabled(features.FeatureCaching) {
		t.Fatal("setup: expected FeatureCaching initially disabled")
	}
	if de.GetCache() != nil {
		t.Fatal("setup: expected no cache while FeatureCaching is disabled")
	}

	if err := de.Configure(WithCaching(true)); err != nil {
		t.Fatalf("Configure(WithCaching(true)) failed: %v", err)
	}

	if !de.IsFeatureEnabled(features.FeatureCaching) {
		t.Fatal("Configure(WithCaching(true)) did not enable FeatureCaching")
	}
	if de.GetCache() == nil {
		t.Fatal("Configure(WithCaching(true)) enabled FeatureCaching but left the cache nil - the flag flip was not detected as cache-affecting")
	}
}

// TestConfigure_CachingFeatureFlipRemovesCache is the reverse direction:
// disabling the FeatureCaching flag (with EnableCache left at its default
// true) must tear the cache down, not leave a stale one in place.
func TestConfigure_CachingFeatureFlipRemovesCache(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}

	if !de.IsFeatureEnabled(features.FeatureCaching) {
		t.Fatal("setup: expected FeatureCaching enabled by default")
	}
	if de.GetCache() == nil {
		t.Fatal("setup: expected a cache instance by default")
	}

	if err := de.Configure(WithCaching(false)); err != nil {
		t.Fatalf("Configure(WithCaching(false)) failed: %v", err)
	}

	if de.IsFeatureEnabled(features.FeatureCaching) {
		t.Fatal("Configure(WithCaching(false)) did not disable FeatureCaching")
	}
	if de.GetCache() != nil {
		t.Fatal("Configure(WithCaching(false)) disabled FeatureCaching but left a cache instance in place")
	}
}
