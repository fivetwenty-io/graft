package yaml_test

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

var pinCalls []yaml.Node

// pinHook records the node each call receives.
type pinHook struct{ Val string }

func (h *pinHook) UnmarshalYAML(n *yaml.Node) error {
	pinCalls = append(pinCalls, *n)
	h.Val = n.Value
	return nil
}

type pinHolder struct {
	H pinHook `yaml:"h"`
	N int     `yaml:"n"`
}

func TestHookRunsOncePerPlace(t *testing.T) {
	pinCalls = nil
	var v struct {
		F pinHook            `yaml:"f"`
		L []pinHook          `yaml:"l"`
		M map[string]pinHook `yaml:"m"`
		P *pinHook           `yaml:"p"`
	}
	if err := yaml.Unmarshal([]byte("f: a\nl: [b, c]\nm: {k: d}\np: e\n"), &v); err != nil {
		t.Fatal(err)
	}
	if len(pinCalls) != 5 || v.F.Val != "a" || v.L[1].Val != "c" || v.M["k"].Val != "d" || v.P.Val != "e" {
		t.Errorf("calls %d, value %+v", len(pinCalls), v)
	}
}

func TestHookUnderMergeSeesTheLaterKey(t *testing.T) {
	pinCalls = nil
	var v struct {
		M pinHolder `yaml:"m"`
	}
	if err := yaml.Unmarshal([]byte("base: &b {h: x}\nm:\n  <<: *b\n  h: y\n"), &v); err != nil {
		t.Fatal(err)
	}
	if len(pinCalls) != 1 || v.M.H.Val != "y" {
		t.Errorf("calls %d, value %+v", len(pinCalls), v)
	}
}

func TestHookUnderAnAlias(t *testing.T) {
	pinCalls = nil
	var v map[string]pinHolder
	if err := yaml.Unmarshal([]byte("a: &x {h: v}\nb: *x\n"), &v); err != nil {
		t.Fatal(err)
	}
	if len(pinCalls) != 2 || v["a"].H.Val != "v" || v["b"].H.Val != "v" {
		t.Errorf("calls %d, value %+v", len(pinCalls), v)
	}
	for _, n := range pinCalls {
		if n.Kind != yaml.ScalarNode || n.Value != "v" {
			t.Errorf("hook saw %+v", n)
		}
	}
}

func TestHookRefusalsRunNoHook(t *testing.T) {
	pinCalls = nil
	var keyed map[pinHook]int
	err := yaml.Unmarshal([]byte("x: 1\n"), &keyed)
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || len(pinCalls) != 0 {
		t.Errorf("hook as a map key: %v, calls %d", err, len(pinCalls))
	}
	var hidden pinHolder
	err = yaml.Unmarshal([]byte("h: v\nn: notanint\n"), &hidden)
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || de.Line != 2 || len(pinCalls) != 0 {
		t.Errorf("goccy error first: %v, calls %d", err, len(pinCalls))
	}
}

func TestHookSeesTimestampAndBinaryTags(t *testing.T) {
	for src, tag := range map[string]string{
		"h: !!timestamp 2026-01-02\n": "!!timestamp",
		"h: !!binary aGk=\n":          "!!binary",
	} {
		pinCalls = nil
		var v pinHolder
		if err := yaml.Unmarshal([]byte(src), &v); err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if len(pinCalls) != 1 || pinCalls[0].Tag != tag {
			t.Errorf("%q: calls %+v", src, pinCalls)
		}
	}
}

func TestNullTagSkipsTheHook(t *testing.T) {
	for _, src := range []string{"h: !!null\n", "h: ~\n", "h: !!null ~\n"} {
		pinCalls = nil
		var v pinHolder
		if err := yaml.Unmarshal([]byte(src), &v); err != nil || len(pinCalls) != 0 || v.H.Val != "" {
			t.Errorf("%q: %v, calls %d", src, err, len(pinCalls))
		}
	}
}
