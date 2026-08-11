package controlflow

import (
	"context"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestE2E_IteratingOverObjects mirrors
// docs/examples/conditional-configs.md's "Iterating Over Objects" example:
// a for-loop nested several levels deep inside static map structure, with
// the loop variable referenced at multiple depths within one iteration.
func TestE2E_IteratingOverObjects(t *testing.T) {
	src := `service_configs:
  - name: api
    port: 8080
    replicas: 3
  - name: worker
    port: 8081
    replicas: 2

kubernetes:
  deployments:
  (( for svc in service_configs ))
    - apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: (( grab svc.name ))
      spec:
        replicas: (( grab svc.replicas ))
        template:
          spec:
            containers:
              - name: (( grab svc.name ))
                port: (( grab svc.port ))
  (( done ))
`
	data := runMergeYAML(t, src)
	k8s := data["kubernetes"].(map[string]interface{})
	deployments := k8s["deployments"].([]interface{})
	if len(deployments) != 2 {
		t.Fatalf("deployments = %#v, want 2 entries", deployments)
	}
	first := deployments[0].(map[string]interface{})
	meta := first["metadata"].(map[string]interface{})
	if meta["name"] != "api" {
		t.Errorf("deployments[0].metadata.name = %v, want api", meta["name"])
	}
	spec := first["spec"].(map[string]interface{})
	if stringifyForCase(spec["replicas"]) != "3" {
		t.Errorf("deployments[0].spec.replicas = %v, want 3", spec["replicas"])
	}
	tmplContainers := spec["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
	c0 := tmplContainers[0].(map[string]interface{})
	if c0["name"] != "api" || stringifyForCase(c0["port"]) != "8080" {
		t.Errorf("deployments[0] container = %#v, want name=api port=8080", c0)
	}
}

// TestE2E_ConditionalsWithGrabAndConcat mirrors "Conditionals with Grab and
// Concat" — control flow composed with ordinary operators inside the
// selected branch.
func TestE2E_ConditionalsWithGrabAndConcat(t *testing.T) {
	src := `meta:
  app_name: my-service
  environment: production
  version: 2.0.0

(( if meta.environment == "production" ))
app:
  name: (( concat meta.app_name "-" meta.environment ))
  image: (( concat "registry.example.com/" meta.app_name ":" meta.version ))
(( else ))
app:
  name: (( concat meta.app_name "-" meta.environment ))
  image: (( concat "localhost/" meta.app_name ":dev" ))
(( fi ))
`
	data := runMergeYAML(t, src)
	app := data["app"].(map[string]interface{})
	if app["name"] != "my-service-production" {
		t.Errorf("app.name = %v, want my-service-production", app["name"])
	}
	if app["image"] != "registry.example.com/my-service:2.0.0" {
		t.Errorf("app.image = %v, want registry.example.com/my-service:2.0.0", app["image"])
	}
}

// TestE2E_ConditionalsWithFallbackOperator mirrors "Vault Secrets with
// Conditionals" structurally (an if-selected branch whose value comes from
// an operator with a "||" fallback) without depending on a live Vault
// service: (( grab )) of a nonexistent path is the same "left operand
// errors, fall back to the right" shape §1.4 documents for "||".
func TestE2E_ConditionalsWithFallbackOperator(t *testing.T) {
	src := `environment: development

database:
  host: db.example.com
  (( if environment == "production" ))
  password: (( grab secrets.prod.password || "prod-fallback" ))
  (( else ))
  password: (( grab secrets.dev.password || "dev-password" ))
  (( fi ))
`
	data := runMergeYAML(t, src)
	db := data["database"].(map[string]interface{})
	if db["password"] != "dev-password" {
		t.Errorf("database.password = %v, want dev-password (missing grab target, || fallback)", db["password"])
	}
}

// TestE2E_KubernetesDeploymentGenerator mirrors the "Real-World Example"
// section: a for-loop over a list of maps, each element used at many
// levels, combined with --prune of the loop-source keys.
func TestE2E_KubernetesDeploymentGenerator(t *testing.T) {
	src := `app:
  name: my-api
  version: 1.5.0

environments:
  - name: development
    namespace: dev
    replicas: 1
  - name: production
    namespace: prod
    replicas: 5

deployments:
(( for env in environments ))
  - metadata:
      name: (( concat app.name "-" env.name ))
      namespace: (( grab env.namespace ))
    spec:
      replicas: (( grab env.replicas ))
(( done ))
`
	data := runMergeYAML(t, src)
	deployments := data["deployments"].([]interface{})
	if len(deployments) != 2 {
		t.Fatalf("deployments = %#v, want 2 entries", deployments)
	}
	d0 := deployments[0].(map[string]interface{})
	meta := d0["metadata"].(map[string]interface{})
	if meta["name"] != "my-api-development" || meta["namespace"] != "dev" {
		t.Errorf("deployments[0].metadata = %#v", meta)
	}
	d1 := deployments[1].(map[string]interface{})
	spec1 := d1["spec"].(map[string]interface{})
	if stringifyForCase(spec1["replicas"]) != "5" {
		t.Errorf("deployments[1].spec.replicas = %v, want 5", spec1["replicas"])
	}
}

// TestE2E_UserPruneStillWorksAlongsideAutoPrune verifies the merge
// pipeline's unconditional "__graft_loop" prune (merge_builder_impl.go)
// does not interfere with a user-specified --prune path.
func TestE2E_UserPruneStillWorksAlongsideAutoPrune(t *testing.T) {
	src := `services:
  - api
  - worker

deployments:
(( for svc in services ))
  - name: (( grab svc ))
(( done ))
`
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	result, err := engine.Merge(context.Background(), doc).WithPrune("services").Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data := result.RawData().(map[string]interface{})
	if _, present := data["services"]; present {
		t.Error("services should have been pruned via --prune")
	}
	if _, present := data["__graft_loop"]; present {
		t.Error("__graft_loop should always be auto-pruned")
	}
	deployments, ok := data["deployments"].([]interface{})
	if !ok || len(deployments) != 2 {
		t.Fatalf("deployments = %#v, want 2 entries (pruning services must not affect it)", data["deployments"])
	}
}

// TestE2E_KeywordAliases exercises elsif/endif/endfor/endwhile/endcase end
// to end, not just at the scanner level.
func TestE2E_KeywordAliases(t *testing.T) {
	src := `a: 1
(( if a == 1 ))
x: matched-if
(( elsif a == 2 ))
x: matched-elsif
(( endif ))

items:
  - one
  - two
out:
(( for i in items ))
  - (( grab i ))
(( endfor ))
`
	data := runMergeYAML(t, src)
	if data["x"] != "matched-if" {
		t.Errorf("x = %v, want matched-if", data["x"])
	}
	out, ok := data["out"].([]interface{})
	if !ok || len(out) != 2 {
		t.Fatalf("out = %#v, want 2 entries", data["out"])
	}
}

// TestE2E_LeadingDocumentStartMarker pins that a source beginning with the
// YAML document-start marker still expands correctly. The __graft_loop
// bindings block has to be emitted inside that document — emitting it above
// the "---" turns a single-document file into a two-document stream whose
// first document holds nothing but the bindings, and graft reads only the
// first document, so the entire user document is silently dropped.
func TestE2E_LeadingDocumentStartMarker(t *testing.T) {
	src := `---
services:
  - api
  - worker
names:
(( for svc in services ))
- (( grab svc ))
(( done ))
`
	data := runMergeYAML(t, src)
	names, ok := data["names"].([]interface{})
	if !ok {
		t.Fatalf("names = %#v, want a 2-element list (document was dropped?)", data)
	}
	if len(names) != 2 || names[0] != "api" || names[1] != "worker" {
		t.Errorf("names = %#v, want [api worker]", names)
	}
	if _, present := data["__graft_loop"]; present {
		t.Errorf("__graft_loop leaked into output: %#v", data)
	}
}

// TestE2E_LeadingCommentsBeforeDocumentStart covers the same insertion point
// with comments and a directive ahead of the "---".
func TestE2E_LeadingCommentsBeforeDocumentStart(t *testing.T) {
	src := `# a leading comment

---
counts:
(( for i in range 1 3 ))
- (( grab i ))
(( done ))
`
	data := runMergeYAML(t, src)
	counts, ok := data["counts"].([]interface{})
	if !ok || len(counts) != 3 {
		t.Fatalf("counts = %#v, want 3 entries", data["counts"])
	}
}

// TestE2E_MarkerWithTrailingComment covers marker lines carrying a trailing
// YAML comment, which the docs' own style permits on any line.
func TestE2E_MarkerWithTrailingComment(t *testing.T) {
	src := `environment: dev
(( if environment == "production" )) # production only
production_only: true
(( fi )) # end of the production block
`
	data := runMergeYAML(t, src)
	if _, present := data["production_only"]; present {
		t.Errorf("production_only emitted for environment=dev: %#v", data)
	}
	if data["environment"] != "dev" {
		t.Errorf("environment = %v, want dev", data["environment"])
	}
}

// TestE2E_SkipEvalKeepsLoopBindings pins that --skip-eval output stays
// re-mergeable. Skipping evaluation leaves the loop bodies' rewritten
// (( grab __graft_loop... )) references unresolved, so the bindings they
// point at have to survive into the intermediate document — pruning them
// there strands every reference, and "graft merge --skip-eval" feeding a
// later graft pass is one of the Genesis drop-in patterns.
func TestE2E_SkipEvalKeepsLoopBindings(t *testing.T) {
	src := `services:
  - api
  - worker
names:
(( for svc in services ))
- (( grab svc ))
(( done ))
`
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	intermediate, err := engine.Merge(context.Background(), doc).SkipEvaluation().Execute()
	if err != nil {
		t.Fatalf("Execute(--skip-eval): %v", err)
	}
	interData, ok := intermediate.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("intermediate is not a map: %#v", intermediate.RawData())
	}
	if _, present := interData["__graft_loop"]; !present {
		t.Fatalf("__graft_loop was pruned from --skip-eval output, stranding its references: %#v", interData)
	}

	// Feeding that intermediate back through a normal pass has to resolve.
	roundTripped, err := graft.MarshalYAML(interData)
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	data := runMergeYAML(t, string(roundTripped))
	names, ok := data["names"].([]interface{})
	if !ok || len(names) != 2 || names[0] != "api" || names[1] != "worker" {
		t.Errorf("names after round trip = %#v, want [api worker]", data["names"])
	}
	if _, present := data["__graft_loop"]; present {
		t.Errorf("__graft_loop leaked into the final output: %#v", data)
	}
}
