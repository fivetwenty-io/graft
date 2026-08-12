package graft

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// capturingLogger records every message passed to it, proving WithLogger
// has an observable effect instead of merely being stored on
// EngineOptions.Logger and never read.
type capturingLogger struct {
	debugMsgs []string
	infoMsgs  []string
	warnMsgs  []string
	errorMsgs []string
}

func (l *capturingLogger) Debug(msg string, _ ...interface{}) { l.debugMsgs = append(l.debugMsgs, msg) }
func (l *capturingLogger) Info(msg string, _ ...interface{})  { l.infoMsgs = append(l.infoMsgs, msg) }
func (l *capturingLogger) Warn(msg string, _ ...interface{})  { l.warnMsgs = append(l.warnMsgs, msg) }
func (l *capturingLogger) Error(msg string, _ ...interface{}) { l.errorMsgs = append(l.errorMsgs, msg) }

// TestWithLogger_ObservableEffect proves WithLogger's configured Logger is
// actually consulted during evaluation, not merely stored and ignored.
func TestWithLogger_ObservableEffect(t *testing.T) {
	logger := &capturingLogger{}
	engine, err := NewEngine(WithLogger(logger))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc := mustParseYAMLDoc(t, engine, "key: value\n")
	if _, err := engine.Evaluate(context.Background(), doc); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if len(logger.debugMsgs) == 0 {
		t.Fatal("WithLogger's Logger received no Debug calls during Evaluate; WithLogger has no observable effect")
	}
}

// TestWithLogger_NilLoggerIsNoOp proves an engine constructed without
// WithLogger (the default) makes no logger calls - i.e. logDebug's nil
// check actually gates the call, rather than logDebug being unconditional
// and only appearing to be gated because every test happens to supply a
// logger.
func TestWithLogger_NilLoggerIsNoOp(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	doc := mustParseYAMLDoc(t, engine, "key: value\n")
	// No logger configured; Evaluate must not panic or otherwise depend on
	// e.opts.Logger being non-nil.
	if _, err := engine.Evaluate(context.Background(), doc); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
}

// TestWithYAMLCompat_ObservableEffect proves WithYAMLCompat changes actual
// parse behavior: with YAML 1.1 boolean conversion disabled, "yes"/"no"
// stay strings instead of becoming booleans.
func TestWithYAMLCompat_ObservableEffect(t *testing.T) {
	yamlSrc := []byte("flag: yes\n")

	withDefault, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defaultDoc, err := withDefault.ParseYAML(yamlSrc)
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}
	if v, err := defaultDoc.GetBool("flag"); err != nil || !v {
		t.Fatalf("default engine: expected flag to parse as boolean true, got value=%v err=%v", v, err)
	}

	withCompatOff, err := NewEngine(WithYAMLCompat(&YAMLCompat{ConvertYAML11Booleans: false}))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	compatOffDoc, err := withCompatOff.ParseYAML(yamlSrc)
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}
	got, err := compatOffDoc.GetString("flag")
	if err != nil {
		t.Fatalf("WithYAMLCompat(ConvertYAML11Booleans: false): expected flag to stay a string, GetString failed: %v", err)
	}
	if got != "yes" {
		t.Fatalf("WithYAMLCompat(ConvertYAML11Booleans: false): flag = %q, want %q", got, "yes")
	}
}

// TestWithYAMLCompat_NilIsIgnored proves a nil compat leaves the engine's
// default YAML 1.1 compatibility behavior in effect instead of disabling
// conversion entirely (a nil *YAMLCompat has no ConvertYAML11Booleans field
// to consult).
func TestWithYAMLCompat_NilIsIgnored(t *testing.T) {
	engine, err := NewEngine(WithYAMLCompat(nil))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	doc, err := engine.ParseYAML([]byte("flag: yes\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}
	if v, err := doc.GetBool("flag"); err != nil || !v {
		t.Fatalf("WithYAMLCompat(nil): expected default conversion behavior (flag=true), got value=%v err=%v", v, err)
	}
}

// TestWithCacheSize_ObservableEffect proves WithCacheSize actually bounds
// the constructed cache's capacity (via eviction), not merely
// EngineOptions.CacheSize.
func TestWithCacheSize_ObservableEffect(t *testing.T) {
	small, err := NewEngine(WithCacheSize(1))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	smallCache := small.(*DefaultEngine).GetCache()
	if smallCache == nil {
		t.Fatal("expected a cache instance (caching enabled by default)")
	}
	for i := 0; i < 200; i++ {
		smallCache.Set(fmt.Sprintf("key-%d", i), i)
	}
	if smallCache.Stats().Evictions == 0 {
		t.Error("WithCacheSize(1): expected evictions after inserting 200 entries, got none - WithCacheSize has no observable effect")
	}

	large, err := NewEngine(WithCacheSize(1_000_000))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	largeCache := large.(*DefaultEngine).GetCache()
	if largeCache == nil {
		t.Fatal("expected a cache instance (caching enabled by default)")
	}
	for i := 0; i < 200; i++ {
		largeCache.Set(fmt.Sprintf("key-%d", i), i)
	}
	if evictions := largeCache.Stats().Evictions; evictions != 0 {
		t.Errorf("WithCacheSize(1_000_000): expected no evictions after inserting 200 entries, got %d", evictions)
	}
	if size := largeCache.Size(); size != 200 {
		t.Errorf("WithCacheSize(1_000_000): expected all 200 entries retained, got Size()=%d", size)
	}
}

// TestWithCacheDisabled_ObservableEffect proves WithCacheDisabled results
// in no cache instance being constructed at all.
func TestWithCacheDisabled_ObservableEffect(t *testing.T) {
	engine, err := NewEngine(WithCacheDisabled())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if c := engine.(*DefaultEngine).GetCache(); c != nil {
		t.Errorf("WithCacheDisabled(): expected no cache instance, got %T", c)
	}
}

// TestWithCacheTTL_ObservableEffect proves WithCacheTTL causes cache
// entries to actually expire, not merely records a TTL value nothing
// consults.
func TestWithCacheTTL_ObservableEffect(t *testing.T) {
	const ttl = 20 * time.Millisecond
	engine, err := NewEngine(WithCacheTTL(ttl))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	c := engine.(*DefaultEngine).GetCache()
	if c == nil {
		t.Fatal("expected a cache instance (caching enabled by default)")
	}

	c.Set("k", "v")
	if _, found := c.Get("k"); !found {
		t.Fatal("expected entry to be present immediately after Set")
	}

	// Poll rather than a single fixed sleep, to tolerate scheduler jitter
	// without inflating the test's worst-case runtime.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, found := c.Get("k"); !found {
			return // expired, as expected
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("WithCacheTTL: entry never expired within 2s of a 20ms TTL")
}

// TestWithOperators_ObservableEffect proves WithOperators' bulk-registered
// operators are actually consulted during evaluation, the same way
// WithCustomOperator's single-operator form is (see
// custom_operator_eval_test.go), and that WithOperators merges into
// (rather than replaces) operators from an earlier WithCustomOperator call.
func TestWithOperators_ObservableEffect(t *testing.T) {
	engine, err := NewEngine(
		WithCustomOperator("upper", &upperOperator{}),
		WithOperators(map[string]Operator{"upper2": &upperOperator{}}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc := mustParseYAMLDoc(t, engine, "a: (( upper \"one\" ))\nb: (( upper2 \"two\" ))\n")
	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if got, err := result.GetString("a"); err != nil || got != "ONE" {
		t.Errorf("a = %q, err = %v; want %q (operator from WithCustomOperator not merged/consulted)", got, err, "ONE")
	}
	if got, err := result.GetString("b"); err != nil || got != "TWO" {
		t.Errorf("b = %q, err = %v; want %q (operator from WithOperators not consulted)", got, err, "TWO")
	}
}

// TestWithOperators_EmptyMapIsNoOp proves an empty/nil map does not
// allocate or otherwise disturb CustomOperators.
func TestWithOperators_EmptyMapIsNoOp(t *testing.T) {
	engine, err := NewEngine(WithOperators(nil))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	doc := mustParseYAMLDoc(t, engine, "key: value\n")
	if _, err := engine.Evaluate(context.Background(), doc); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
}

// TestWithMaxWorkers_AliasOfWithConcurrency proves the deprecated
// WithMaxWorkers still sets exactly the field WithConcurrency does.
func TestWithMaxWorkers_AliasOfWithConcurrency(t *testing.T) {
	viaAlias, err := NewEngine(WithMaxWorkers(7))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	viaCanonical, err := NewEngine(WithConcurrency(7))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if got := viaAlias.(*DefaultEngine).opts.MaxConcurrency; got != 7 {
		t.Errorf("WithMaxWorkers(7): MaxConcurrency = %d, want 7", got)
	}
	if got := viaCanonical.(*DefaultEngine).opts.MaxConcurrency; got != 7 {
		t.Errorf("WithConcurrency(7): MaxConcurrency = %d, want 7", got)
	}
}

// TestDeprecatedOptions_StillCompileAndConstruct is the "assert they still
// compile and don't crash" coverage the deprecated Vault/AWS/memory-pool
// options need: each is applied to a real NewEngine call and must not
// error or panic, even though (per their doc comments) none has an
// observable effect - WithVault/WithVaultTarget/WithAWS/WithAWSTarget
// (backend_vault_test.go, backend_aws_test.go,
// backend_vault_engine_test.go, backend_aws_engine_test.go) are the real,
// working replacements for the Vault/AWS ones below; there is no
// replacement for WithMaxWorkers other than WithConcurrency, and none for
// WithMemoryPools.
func TestDeprecatedOptions_StillCompileAndConstruct(t *testing.T) {
	deprecated := []struct {
		name string
		opt  EngineOption
	}{
		{"WithVaultClient", WithVaultClient(nil)},
		{"WithAWSConfig", WithAWSConfig(&AWSConfig{Region: "us-east-1"})},
		{"WithVaultConfig", WithVaultConfig("https://vault.example.com", "token")},
		{"WithAWSRegion", WithAWSRegion("us-east-1")},
		{"WithVaultSkipTLS", WithVaultSkipTLS(true)},
		{"WithAWSProfile", WithAWSProfile("default")},
		{"WithMemoryPools", WithMemoryPools(true)},
		{"WithMaxWorkers", WithMaxWorkers(2)},
	}

	for _, d := range deprecated {
		t.Run(d.name, func(t *testing.T) {
			engine, err := NewEngine(d.opt)
			if err != nil {
				t.Fatalf("%s: NewEngine failed: %v", d.name, err)
			}
			if engine == nil {
				t.Fatalf("%s: NewEngine returned a nil engine", d.name)
			}
			doc := mustParseYAMLDoc(t, engine, "key: value\n")
			if _, err := engine.Evaluate(context.Background(), doc); err != nil {
				t.Fatalf("%s: Evaluate failed on an otherwise-trivial document: %v", d.name, err)
			}
		})
	}
}

// TestNewEngine_DefaultsMatchDefaultEngineOpts proves NewEngine() and
// NewDefaultEngine() (via defaultEngineOpts) now share one default
// configuration instead of the two different default sets that existed
// before (CacheSize 1000/MaxConcurrency 10 vs 10000/4).
func TestNewEngine_DefaultsMatchDefaultEngineOpts(t *testing.T) {
	want := defaultEngineOpts()

	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	got := engine.(*DefaultEngine).opts

	if got.EnableCache != want.EnableCache {
		t.Errorf("EnableCache = %v, want %v", got.EnableCache, want.EnableCache)
	}
	if got.CacheSize != want.CacheSize {
		t.Errorf("CacheSize = %d, want %d", got.CacheSize, want.CacheSize)
	}
	if got.EnableParallel != want.EnableParallel {
		t.Errorf("EnableParallel = %v, want %v", got.EnableParallel, want.EnableParallel)
	}
	if got.MaxConcurrency != want.MaxConcurrency {
		t.Errorf("MaxConcurrency = %d, want %d", got.MaxConcurrency, want.MaxConcurrency)
	}
	if got.DataflowOrder != want.DataflowOrder {
		t.Errorf("DataflowOrder = %q, want %q", got.DataflowOrder, want.DataflowOrder)
	}
}

// TestCreateDefaultEngine_MatchesNewEngineDefaults proves CreateDefaultEngine
// no longer carries its own, third default set (it used to hardcode
// CacheSize 1000/MaxConcurrency 10 regardless of NewEngine's own defaults).
func TestCreateDefaultEngine_MatchesNewEngineDefaults(t *testing.T) {
	viaCreate, err := CreateDefaultEngine()
	if err != nil {
		t.Fatalf("CreateDefaultEngine failed: %v", err)
	}
	viaNew, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	got := viaCreate.(*DefaultEngine).opts
	want := viaNew.(*DefaultEngine).opts
	// EngineOptions contains a map field (CustomOperators), so it cannot be
	// compared with == / !=; compare the fields default drift was actually
	// about instead.
	if got.EnableCache != want.EnableCache || got.CacheSize != want.CacheSize ||
		got.EnableParallel != want.EnableParallel || got.MaxConcurrency != want.MaxConcurrency ||
		got.DataflowOrder != want.DataflowOrder {
		t.Errorf("CreateDefaultEngine() opts = %+v, want %+v (NewEngine() with no options)", got, want)
	}
}
