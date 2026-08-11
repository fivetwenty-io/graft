package controlflow

import "testing"

func TestExpand_Case_BasicMatch(t *testing.T) {
	src := `cloud_provider: aws

(( case cloud_provider ))
(( when "aws" ))
storage:
  type: s3
  class: STANDARD
(( when "gcp" ))
storage:
  type: gcs
(( when "azure" ))
storage:
  type: blob
(( default ))
storage:
  type: local
(( esac ))
`
	data := runMergeYAML(t, src)
	storage := data["storage"].(map[string]interface{})
	if storage["type"] != "s3" || storage["class"] != "STANDARD" {
		t.Errorf("storage = %#v, want type: s3, class: STANDARD", storage)
	}
}

func TestExpand_Case_DefaultFallback(t *testing.T) {
	src := `cloud_provider: digitalocean

(( case cloud_provider ))
(( when "aws" ))
storage:
  type: s3
(( default ))
storage:
  type: local
(( esac ))
`
	data := runMergeYAML(t, src)
	storage := data["storage"].(map[string]interface{})
	if storage["type"] != "local" {
		t.Errorf("storage.type = %v, want local", storage["type"])
	}
}

func TestExpand_Case_MultiplePatternsPerWhen(t *testing.T) {
	src := `environment: prod

(( case environment ))
(( when "prod" | "production" ))
settings:
  replicas: 5
(( when "stg" | "staging" | "uat" ))
settings:
  replicas: 2
(( default ))
settings:
  replicas: 1
(( esac ))
`
	data := runMergeYAML(t, src)
	settings := data["settings"].(map[string]interface{})
	if stringifyForCase(settings["replicas"]) != "5" {
		t.Errorf("settings.replicas = %v, want 5", settings["replicas"])
	}
}

func TestExpand_Case_NoMatchNoDefault_EmitsNothing(t *testing.T) {
	src := `cloud_provider: unknown-cloud

(( case cloud_provider ))
(( when "aws" ))
storage:
  type: s3
(( esac ))
placeholder: 1
`
	data := runMergeYAML(t, src)
	if _, present := data["storage"]; present {
		t.Errorf("storage should be entirely absent when nothing matches and there is no default, got %#v", data["storage"])
	}
}

func TestExpand_Case_FirstMatchWins(t *testing.T) {
	// C-14: no fallthrough. Two "when" clauses both spell out patterns that
	// could match; only the first is honored.
	src := `env: prod

(( case env ))
(( when "prod" ))
picked: first
(( when "prod" | "other" ))
picked: second
(( esac ))
`
	data := runMergeYAML(t, src)
	if data["picked"] != "first" {
		t.Errorf("picked = %v, want first", data["picked"])
	}
}

func TestExpand_Case_NestedCase(t *testing.T) {
	src := `deployment:
  type: kubernetes
  size: large

(( case deployment.type ))
(( when "kubernetes" ))
platform: k8s
  (( case deployment.size ))
  (( when "small" ))
resources:
  cpu: 100m
  (( when "medium" ))
resources:
  cpu: 500m
  (( when "large" ))
resources:
  cpu: 1000m
  (( esac ))
(( when "docker" ))
platform: docker-compose
(( esac ))
`
	data := runMergeYAML(t, src)
	if data["platform"] != "k8s" {
		t.Errorf("platform = %v, want k8s", data["platform"])
	}
	resources := data["resources"].(map[string]interface{})
	if resources["cpu"] != "1000m" {
		t.Errorf("resources.cpu = %v, want 1000m", resources["cpu"])
	}
}

func TestExpand_Case_NumericAndBooleanPatterns(t *testing.T) {
	src := `count: 3
flag: true

(( case count ))
(( when 3 ))
matched_count: true
(( esac ))

(( case flag ))
(( when true ))
matched_flag: true
(( esac ))
`
	data := runMergeYAML(t, src)
	if data["matched_count"] != true {
		t.Errorf("matched_count = %v, want true", data["matched_count"])
	}
	if data["matched_flag"] != true {
		t.Errorf("matched_flag = %v, want true", data["matched_flag"])
	}
}

func TestExpand_Case_BareIdentifierPattern_Errors(t *testing.T) {
	// C-12: when-patterns are literals only (STRING/NUMBER/BOOLEAN); a bare
	// identifier is rejected rather than treated ambiguously as a reference.
	err := runMergeYAMLErr(t, "cloud: aws\n(( case cloud ))\n(( when aws ))\nx: 1\n(( esac ))\n")
	if err == nil {
		t.Fatal("expected an error for a bare-identifier when-pattern")
	}
}

func TestExpand_Case_DefaultNotLast_Errors(t *testing.T) {
	err := runMergeYAMLErr(t, "cloud: aws\n(( case cloud ))\n(( default ))\nx: 1\n(( when \"aws\" ))\nx: 2\n(( esac ))\n")
	if err == nil {
		t.Fatal("expected an error for a default clause not placed last")
	}
}
