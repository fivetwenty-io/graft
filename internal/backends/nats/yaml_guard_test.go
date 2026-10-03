package natsbackend

import (
	"strings"
	"testing"
	"time"
)

// TestUnmarshalGuardedRejectsDeepNesting feeds the guard a value that
// nests 20,000 flow sequences deep, which an unguarded yaml.Unmarshal
// spends close to a gigabyte on. The guard has to refuse it with the
// max-depth error and do so quickly.
func TestUnmarshalGuardedRejectsDeepNesting(t *testing.T) {
	data := []byte("a: " + strings.Repeat("[", 20000))

	var out interface{}
	start := time.Now()
	err := unmarshalGuarded(data, &out)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("unmarshalGuarded accepted 20,000 nested [, got %v", out)
	}
	if !strings.Contains(err.Error(), "exceeded max depth") {
		t.Errorf("error = %q, want the max depth error", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("took %s to refuse the value, want a fast failure", elapsed)
	}
}

// TestUnmarshalGuardedDecodesNormalValues checks that an ordinary
// document decodes the way yaml.Unmarshal alone decodes it, and that
// only the first document of several is read.
func TestUnmarshalGuardedDecodesNormalValues(t *testing.T) {
	var out map[string]interface{}
	if err := unmarshalGuarded([]byte("name: web\nports:\n- 80\n- 443\n---\nname: other\n"), &out); err != nil {
		t.Fatalf("unmarshalGuarded: %v", err)
	}
	if out["name"] != "web" {
		t.Errorf("name = %v, want web", out["name"])
	}
	if ports, ok := out["ports"].([]interface{}); !ok || len(ports) != 2 {
		t.Errorf("ports = %v, want two entries", out["ports"])
	}
}
