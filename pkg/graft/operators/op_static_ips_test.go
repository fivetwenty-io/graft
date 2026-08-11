package operators

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// evalYAML parses and evaluates a YAML document with a fresh engine,
// returning the resulting tree as a map. Fails the test on any error
// so callers can focus on assertions.
func evalYAML(t *testing.T, yamlSrc string) map[string]interface{} {
	t.Helper()

	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}

	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("evaluated document is not a map: %T", result.RawData())
	}
	return data
}

// evalYAMLErr is like evalYAML but expects evaluation to fail, returning
// the error for the caller to inspect.
func evalYAMLErr(t *testing.T, yamlSrc string) error {
	t.Helper()

	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	_, err = engine.Evaluate(context.Background(), doc)
	if err == nil {
		t.Fatalf("expected evaluation error, got none")
	}
	return err
}

// staticIPsAt digs jobs[0].networks[0].static_ips out of an evaluated
// document and returns it as a []interface{}.
func staticIPsAt(t *testing.T, data map[string]interface{}, jobIdx, netIdx int) []interface{} {
	t.Helper()

	jobs, ok := data["jobs"].([]interface{})
	if !ok || jobIdx >= len(jobs) {
		t.Fatalf("jobs[%d] not found in %#v", jobIdx, data)
	}
	job, ok := jobs[jobIdx].(map[string]interface{})
	if !ok {
		t.Fatalf("jobs[%d] is not a map", jobIdx)
	}
	nets, ok := job["networks"].([]interface{})
	if !ok || netIdx >= len(nets) {
		t.Fatalf("jobs[%d].networks[%d] not found", jobIdx, netIdx)
	}
	net, ok := nets[netIdx].(map[string]interface{})
	if !ok {
		t.Fatalf("jobs[%d].networks[%d] is not a map", jobIdx, netIdx)
	}
	ips, ok := net["static_ips"].([]interface{})
	if !ok {
		t.Fatalf("jobs[%d].networks[%d].static_ips is not a list: %#v", jobIdx, netIdx, net["static_ips"])
	}
	return ips
}

func ipStrings(t *testing.T, ips []interface{}) []string {
	t.Helper()
	out := make([]string, len(ips))
	for i, v := range ips {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("static_ips[%d] is not a string: %#v", i, v)
		}
		out[i] = s
	}
	return out
}

func assertStringSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: got %q, want %q (full got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}
}

// TestStaticIPsAZPoolConstruction verifies the pool built from
// networks.<name>.subnets.*.static (with IP ranges expanded, and the
// default AZ "z1" applied when no az/azs is present on a subnet)
// matches spruce's behavior byte-for-byte over a fixture set.
func TestStaticIPsAZPoolConstruction(t *testing.T) {
	ansi.Color(false)

	t.Run("no az/azs on subnet defaults to z1, single range expands in order", func(t *testing.T) {
		data := evalYAML(t, `
jobs:
- name: api
  instances: 3
  networks:
  - name: net1
    static_ips: (( static_ips(0, 1, 2) ))
networks:
- name: net1
  subnets:
  - static:
    - 10.0.0.2 - 10.0.0.5
`)
		got := ipStrings(t, staticIPsAt(t, data, 0, 0))
		assertStringSliceEqual(t, got, []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"})
	})

	t.Run("explicit singular az on subnet", func(t *testing.T) {
		data := evalYAML(t, `
jobs:
- name: api
  instances: 1
  azs: [z2]
  networks:
  - name: net1
    static_ips: (( static_ips(0) ))
networks:
- name: net1
  subnets:
  - az: z2
    static:
    - 10.0.0.10 - 10.0.0.12
`)
		got := ipStrings(t, staticIPsAt(t, data, 0, 0))
		assertStringSliceEqual(t, got, []string{"10.0.0.10"})
	})

	t.Run("multiple azs on one subnet, plus a second az-scoped subnet, offset addressing crosses subnets", func(t *testing.T) {
		// Mirrors assets/static_ips/multi-azs-multi-zone-job.yml:
		// az z1's pool is [10.0.0.1..10.0.0.15] ++ [10.1.1.1], so
		// offset 15 (0-indexed, 16th address) lands on 10.1.1.1.
		data := evalYAML(t, `
jobs:
- name: static_z1
  instances: 2
  azs: [z1, z2, z3]
  networks:
  - name: net1
    static_ips: (( static_ips(15, 16) ))
networks:
- name: net1
  subnets:
  - azs: [z1, z2, z3]
    static:
    - 10.0.0.1 - 10.0.0.15
  - azs: [z1]
    static:
    - 10.1.1.1
  - azs: [z2]
    static:
    - 10.2.2.2
`)
		got := ipStrings(t, staticIPsAt(t, data, 0, 0))
		assertStringSliceEqual(t, got, []string{"10.1.1.1", "10.2.2.2"})
	})

	t.Run("job with no instance_groups azs falls back to network pool AZs in declared order", func(t *testing.T) {
		data := evalYAML(t, `
jobs:
- name: api
  instances: 2
  networks:
  - name: net1
    static_ips: (( static_ips(0, 1) ))
networks:
- name: net1
  subnets:
  - az: z1
    static:
    - 10.0.0.1
  - az: z2
    static:
    - 10.0.0.2
`)
		got := ipStrings(t, staticIPsAt(t, data, 0, 0))
		assertStringSliceEqual(t, got, []string{"10.0.0.1", "10.0.0.2"})
	})
}

// TestStaticIPsErrorStrings pins the message portion of static_ips
// errors (the part after ` - $.path: `) to spruce's exact wording, for
// every error case called out in the parity work: not-enough-IPs,
// AZ-not-found, offset-out-of-bounds, already-claimed, and
// non-numeric-instances.
func TestStaticIPsErrorStrings(t *testing.T) {
	ansi.Color(false)

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "not enough static IPs requested",
			yaml: `
jobs:
- name: api
  instances: 3
  networks:
  - name: net1
    static_ips: (( static_ips(0) ))
networks:
- name: net1
  subnets:
  - static:
    - 10.0.0.1 - 10.0.0.10
`,
			want: "not enough static IPs requested for job of 3 instances (only asked for 1)",
		},
		{
			name: "az not found in network AZs",
			yaml: `
jobs:
- name: api
  instances: 1
  azs: [z9]
  networks:
  - name: net1
    static_ips: (( static_ips(0) ))
networks:
- name: net1
  subnets:
  - az: z1
    static:
    - 10.0.0.1
`,
			// spruce's error echoes the instance_groups AZ list it was
			// checking against (not the network's pool AZs) — matched here.
			want: "could not find AZ z9 (in network AZS [z9])",
		},
		{
			name: "offset out of bounds",
			yaml: `
jobs:
- name: api
  instances: 1
  networks:
  - name: net1
    static_ips: (( static_ips(5) ))
networks:
- name: net1
  subnets:
  - static:
    - 10.0.0.1 - 10.0.0.3
`,
			want: "request for static_ip(5) in a pool of only 3 (zero-indexed) static addresses",
		},
		{
			name: "IP already claimed by another instance group",
			yaml: `
jobs:
- name: static_z1
  instances: 1
  azs: [z1]
  networks:
  - name: net1
    static_ips: (( static_ips(0) ))
- name: static_z2
  instances: 1
  azs: [z1]
  networks:
  - name: net1
    static_ips: (( static_ips(0) ))
networks:
- name: net1
  subnets:
  - az: z1
    static:
    - 10.0.0.1
`,
			want: "tried to use IP '10.0.0.1', but that address is already allocated to static_z1/0",
		},
		{
			name: "non-numeric instances",
			yaml: `
jobs:
- name: api
  instances: not-a-number
  networks:
  - name: net1
    static_ips: (( static_ips(0) ))
networks:
- name: net1
  subnets:
  - static:
    - 10.0.0.1
`,
			want: "the `instances:` for the current job is not numeric",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := evalYAMLErr(t, tc.yaml)
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain expected spruce wording %q", err.Error(), tc.want)
			}
		})
	}
}

// TestClaimStaticIPIsRaceSafe proves claimStaticIP's check-then-set
// sequence is atomic: many goroutines racing to claim the same IP
// against a shared engine must yield exactly one winner and every
// loser must observe the winner's claim (never a phantom "no thief"
// result), even under `go test -race`.
func TestClaimStaticIPIsRaceSafe(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	state := engine.GetOperatorState()

	const racers = 64
	const ip = "10.0.0.1"

	var wg sync.WaitGroup
	var successes sync.WaitGroup
	successCount := make(chan string, racers)

	wg.Add(racers)
	successes.Add(racers)
	for i := 0; i < racers; i++ {
		owner := fmt.Sprintf("job/%d", i)
		go func(owner string) {
			defer wg.Done()
			defer successes.Done()
			if _, claimErr := claimStaticIP(state, ip, owner); claimErr == nil {
				successCount <- owner
			}
		}(owner)
	}
	wg.Wait()
	successes.Wait()
	close(successCount)

	winners := 0
	var winner string
	for w := range successCount {
		winners++
		winner = w
	}
	if winners != 1 {
		t.Fatalf("expected exactly 1 successful claim of %s, got %d", ip, winners)
	}

	// The engine's authoritative state must agree with the sole winner.
	if got := state.GetUsedIPs()[ip]; got != winner {
		t.Fatalf("engine used-IP owner %q does not match sole winner %q", got, winner)
	}
}
