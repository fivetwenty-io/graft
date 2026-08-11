package controlflow

import "testing"

func TestExpand_Range_Basic(t *testing.T) {
	src := `workers:
(( for i in range 1 5 ))
  - name: (( concat "worker-" i ))
    id: (( grab i ))
(( done ))
`
	data := runMergeYAML(t, src)
	workers := data["workers"].([]interface{})
	if len(workers) != 5 {
		t.Fatalf("workers = %#v, want 5 entries (closed interval [1,5])", workers)
	}
	for i, raw := range workers {
		d := raw.(map[string]interface{})
		wantID := i + 1
		if stringifyForCase(d["id"]) != stringifyForCase(wantID) {
			t.Errorf("workers[%d].id = %v, want %d", i, d["id"], wantID)
		}
		wantName := "worker-" + stringifyForCase(wantID)
		if d["name"] != wantName {
			t.Errorf("workers[%d].name = %v, want %v", i, d["name"], wantName)
		}
	}
}

func TestExpand_Range_WithStep(t *testing.T) {
	src := `ports:
(( for port in range 8080 8090 2 ))
  - (( grab port ))
(( done ))
`
	data := runMergeYAML(t, src)
	ports := data["ports"].([]interface{})
	want := []int{8080, 8082, 8084, 8086, 8088, 8090}
	if len(ports) != len(want) {
		t.Fatalf("ports = %#v, want %d entries", ports, len(want))
	}
	for i, raw := range ports {
		if stringifyForCase(raw) != stringifyForCase(want[i]) {
			t.Errorf("ports[%d] = %v, want %d", i, raw, want[i])
		}
	}
}

func TestExpand_Range_ExpressionBounds(t *testing.T) {
	// C-4: range bounds may be any expression that evaluates to an integer,
	// not just numeric literals.
	src := `max_retries: 4
base_delay: 1

retries:
(( for attempt in range 0 max_retries ))
  - attempt: (( grab attempt ))
    delay: (( calc "base_delay * pow(2, attempt)" ))
(( done ))
`
	data := runMergeYAML(t, src)
	retries := data["retries"].([]interface{})
	if len(retries) != 5 {
		t.Fatalf("retries = %#v, want 5 entries (range 0..4 inclusive)", retries)
	}
	wantDelay := []int{1, 2, 4, 8, 16}
	for i, raw := range retries {
		d := raw.(map[string]interface{})
		if stringifyForCase(d["attempt"]) != stringifyForCase(i) {
			t.Errorf("retries[%d].attempt = %v, want %d", i, d["attempt"], i)
		}
		if stringifyForCase(d["delay"]) != stringifyForCase(wantDelay[i]) {
			t.Errorf("retries[%d].delay = %v, want %d", i, d["delay"], wantDelay[i])
		}
	}
}

func TestExpand_Range_StepZero_Errors(t *testing.T) {
	err := runMergeYAMLErr(t, "out:\n(( for i in range 1 5 0 ))\n  - (( grab i ))\n(( done ))\n")
	if err == nil {
		t.Fatal("expected an error for a zero step")
	}
}

func TestExpand_Range_WrongSignStep_Errors(t *testing.T) {
	err := runMergeYAMLErr(t, "out:\n(( for i in range 5 1 1 ))\n  - (( grab i ))\n(( done ))\n")
	if err == nil {
		t.Fatal("expected an error for a step that does not move start toward end")
	}
}

func TestExpand_Range_TwoVars_Errors(t *testing.T) {
	// C-7: range yields a single value per iteration; two loop variables
	// over a range is an error.
	err := runMergeYAMLErr(t, "out:\n(( for a, b in range 1 3 ))\n  - x: 1\n(( done ))\n")
	if err == nil {
		t.Fatal("expected an error for two loop variables over a range")
	}
}
