// Command gen-workload writes a deterministic, Genesis-shaped merge
// workload for scripts/byte-identity.sh: one large base manifest plus a
// stack of overlays, including name-keyed array merges, list operators,
// grab/concat operators with valid targets, and go-patch operation
// files. Output depends only on the code, never on time or randomness,
// so two runs always produce identical inputs.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var words = []string{
	"router", "diego", "cell", "cloud", "controller", "uaa", "nats",
	"doppler", "loggregator", "syslog", "credhub", "adapter", "scheduler",
	"api", "worker", "consul", "blobstore", "database", "gorouter", "silk",
	"bbs", "auctioneer", "locket", "rep", "emitter", "tps", "proxy",
	"metron", "agent", "policy", "server", "vxlan",
}

func word(i int) string { return words[i%len(words)] }

// scalar returns a varied deterministic YAML scalar for slot i at depth d.
func scalar(i, d int) string {
	switch (i*7 + d*3) % 11 {
	case 0:
		if i%2 == 0 {
			return "true"
		}
		return "false"
	case 1:
		return fmt.Sprintf("%d", i*1024+d)
	case 2:
		return fmt.Sprintf("%d.%d", i%97, d+1)
	case 3:
		return fmt.Sprintf("%s-%s-%d", word(i), word(i+3), i)
	case 4:
		return fmt.Sprintf("\"quoted %s value %d\"", word(i+5), i)
	case 5:
		return fmt.Sprintf("https://%s.%s.internal:%d/path", word(i), word(i+1), 8000+i%1000)
	case 6:
		return fmt.Sprintf("10.244.%d.%d", i%256, (i*3)%256)
	case 7:
		return "((placeholder))"
	case 8:
		return fmt.Sprintf("'single %s %d'", word(i+2), i)
	case 9:
		return fmt.Sprintf("%s_%s", word(i), word(i+7))
	default:
		return fmt.Sprintf("val-%d-%d", i, d)
	}
}

func props(b *strings.Builder, indent, depth, maxDepth, seed, width int) {
	pad := strings.Repeat("  ", indent)
	for i := 0; i < width; i++ {
		key := fmt.Sprintf("%s_%d", word(seed+i), i)
		if depth < maxDepth && i%3 == 0 {
			fmt.Fprintf(b, "%s%s:\n", pad, key)
			props(b, indent+1, depth+1, maxDepth, seed+i*11+1, width-1)
		} else if i%5 == 4 {
			fmt.Fprintf(b, "%s%s:\n", pad, key)
			for j := 0; j < 4; j++ {
				fmt.Fprintf(b, "%s- %s\n", pad, scalar(seed+i+j, depth))
			}
		} else {
			fmt.Fprintf(b, "%s%s: %s\n", pad, key, scalar(seed+i, depth))
		}
	}
}

func genBig(path string) error {
	var b strings.Builder
	b.WriteString("name: byte-identity-workload\n\nmeta:\n")
	b.WriteString("  environment: ci\n  network: default\n")
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&b, "  %s: %s-%d\n", word(i), word(i+4), i)
	}
	b.WriteString("\nreleases:\n")
	for i := 0; i < 24; i++ {
		fmt.Fprintf(&b, "- name: %s-release\n  version: %d.%d.%d\n", word(i), i, i%7, i%3)
	}
	b.WriteString("\ninstance_groups:\n")
	for g := 0; g < 24; g++ {
		fmt.Fprintf(&b, "- name: %s-%d\n  instances: %d\n  azs: [z1, z2]\n", word(g), g, 1+g%4)
		fmt.Fprintf(&b, "  grabbed: (( grab meta.%s ))\n", word(g%16))
		fmt.Fprintf(&b, "  joined: (( concat meta.environment \"-\" \"%s\" ))\n", word(g))
		b.WriteString("  properties:\n")
		var p strings.Builder
		props(&p, 2, 0, 4, g*131, 7)
		b.WriteString(p.String())
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func genOverlay(path string, n int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "meta:\n  overlay_%d: applied\n  %s: patched-%d\n", n, word(n), n)
	b.WriteString("\ninstance_groups:\n")
	for k := 0; k < 3; k++ {
		g := (n*5 + k*7) % 24
		fmt.Fprintf(&b, "- name: %s-%d\n  instances: %d\n", word(g), g, 1+(n+k)%5)
		fmt.Fprintf(&b, "  properties:\n    %s_0: %s\n", word(g*131), scalar(n*17+k, 1))
	}
	if n%4 == 0 {
		b.WriteString("\nreleases:\n- (( append ))\n")
		fmt.Fprintf(&b, "- name: extra-%d\n  version: 0.%d.0\n", n, n)
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func genGoPatch(path string, n int) error {
	var b strings.Builder
	for k := 0; k < 4; k++ {
		fmt.Fprintf(&b, "- type: replace\n  path: /meta/patched_%d_%d?\n  value: gp-%d-%d\n", n, k, n, k)
	}
	fmt.Fprintf(&b, "- type: replace\n  path: /meta/%s\n  value: go-patched-%d\n", word(n), n)
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// genDense writes an operator-dense single document: ~1100 operator
// calls over a small data section, mixing independent grabs/concats
// with chained references, joins over lists, and calc arithmetic. The
// merge phase is trivial here by design - evaluation dominates - so
// this file is the workload for measuring parallel-evaluation changes
// (worker pool sizing, scheduler behavior) where big.yml measures
// parse/merge.
func genDense(path string) error {
	var b strings.Builder
	b.WriteString("name: dense-eval-workload\n\nmeta:\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "  m%03d: %s-%d\n", i, word(i), i)
	}
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "  n%02d: %d\n", i, i*3+1)
	}
	b.WriteString("  list:\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&b, "  - %s\n", word(i))
	}

	b.WriteString("\ngrabs:\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "  g%03d: (( grab meta.m%03d ))\n", i, i)
	}
	b.WriteString("\nconcats:\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "  c%03d: (( concat meta.m%03d \"/\" meta.m%03d ))\n", i, i, (i+37)%200)
	}
	b.WriteString("\nchains:\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "  h%03d: (( grab grabs.g%03d ))\n", i, i)
	}
	b.WriteString("\ndeeper:\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "  d%03d: (( concat chains.h%03d \"+\" concats.c%03d ))\n", i, i, i)
	}
	b.WriteString("\njoins:\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "  j%03d: (( join \",\" meta.list ))\n", i)
	}
	b.WriteString("\ncalcs:\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "  k%03d: (( calc \"meta.n%02d * 2 + %d\" ))\n", i, i%50, i)
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gen-workload <output-dir>")
		os.Exit(1)
	}
	dir := filepath.Clean(os.Args[1])
	if err := os.MkdirAll(dir, 0o750); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fail := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fail(genBig(filepath.Join(dir, "big.yml")))
	for n := 1; n <= 40; n++ {
		fail(genOverlay(filepath.Join(dir, fmt.Sprintf("o%02d.yml", n)), n))
	}
	for n := 41; n <= 42; n++ {
		fail(genGoPatch(filepath.Join(dir, fmt.Sprintf("o%02d.yml", n)), n))
	}
	fail(genDense(filepath.Join(dir, "dense.yml")))
}
