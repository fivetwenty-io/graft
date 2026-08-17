package graft_test

import (
	"testing"
)

// These tests pin spruce's BOSH-placeholder pass-through behavior: any
// (( ... )) whose contents are a bare reference (dotted or not) — the shape
// BOSH/CredHub variable placeholders take in cf-deployment manifests — is
// NOT an operator call and must survive a merge byte-for-byte, whether or
// not a matching key exists in the document. Spruce only evaluates
// (( <registered-op> [args...] )); everything else flows through untouched
// so a later tool (BOSH director, CredHub, genesis) can interpolate it.

func passthroughGet(t *testing.T, yamlSrc string) interface{} {
	t.Helper()
	doc, err := mergeYAML(t, yamlSrc)
	if err != nil {
		t.Fatalf("unexpected merge error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	return got
}

func TestBoshPlaceholderPassthrough(t *testing.T) {
	t.Run("tight dotted placeholder with no matching key passes through verbatim", func(t *testing.T) {
		got := passthroughGet(t, "x: ((forwarder_agent_metrics_tls.ca))\n")
		if got != "((forwarder_agent_metrics_tls.ca))" {
			t.Fatalf("expected verbatim placeholder, got %v (%T)", got, got)
		}
	})

	t.Run("tight dotted placeholder passes through even when the key exists", func(t *testing.T) {
		got := passthroughGet(t, "meta:\n  name: hello\nx: ((meta.name))\n")
		if got != "((meta.name))" {
			t.Fatalf("expected verbatim placeholder (spruce never implicitly grabs), got %v (%T)", got, got)
		}
	})

	t.Run("spaced dotted reference passes through verbatim with original spacing", func(t *testing.T) {
		got := passthroughGet(t, "meta:\n  name: hello\nx: (( meta.name ))\n")
		if got != "(( meta.name ))" {
			t.Fatalf("expected verbatim '(( meta.name ))', got %v (%T)", got, got)
		}
	})

	t.Run("tight single-name placeholder passes through verbatim without respacing", func(t *testing.T) {
		// BOSH's placeholder regex does not allow interior whitespace, so
		// ((cf_admin_password)) must not be re-rendered as (( cf_admin_password )).
		got := passthroughGet(t, "x: ((cf_admin_password))\n")
		if got != "((cf_admin_password))" {
			t.Fatalf("expected verbatim '((cf_admin_password))', got %v (%T)", got, got)
		}
	})

	t.Run("quoted placeholder value behaves the same as unquoted", func(t *testing.T) {
		got := passthroughGet(t, "x: \"((loggregator_tls_agent.certificate))\"\n")
		if got != "((loggregator_tls_agent.certificate))" {
			t.Fatalf("expected verbatim placeholder, got %v (%T)", got, got)
		}
	})

	t.Run("explicit grab still resolves", func(t *testing.T) {
		got := passthroughGet(t, "meta:\n  name: hello\nx: (( grab meta.name ))\n")
		if got != "hello" {
			t.Fatalf("expected grab to resolve to 'hello', got %v (%T)", got, got)
		}
	})

	t.Run("genesis entombed placeholder with slashes and dashes passes through verbatim", func(t *testing.T) {
		// genesis's Credhub entombment rewrites vault operators into
		// BOSH placeholders like ((genesis-entombed/uaa_ssl--key--fe75a2d0)).
		// The inner text is not parseable as a graft expression; spruce
		// passes it through untouched and graft must too.
		got := passthroughGet(t, "x: ((genesis-entombed/uaa_ssl--key--fe75a2d0))\n")
		if got != "((genesis-entombed/uaa_ssl--key--fe75a2d0))" {
			t.Fatalf("expected verbatim entombed placeholder, got %v (%T)", got, got)
		}
	})

	t.Run("absolute credhub-style placeholder passes through verbatim", func(t *testing.T) {
		got := passthroughGet(t, "x: ((/dns_healthcheck_tls.ca))\n")
		if got != "((/dns_healthcheck_tls.ca))" {
			t.Fatalf("expected verbatim placeholder, got %v (%T)", got, got)
		}
	})

	t.Run("malformed expression starting with a registered operator still errors", func(t *testing.T) {
		_, err := mergeYAML(t, "x: (( concat \"unterminated ))\n")
		if err == nil {
			t.Fatalf("expected a parse error for a malformed operator-leading expression")
		}
	})

	t.Run("dotted reference segment containing a plus resolves as one path", func(t *testing.T) {
		// genesis's vaultified manifests write map keys like
		// "haproxy_ssl+certificate" and reference them as
		// (( concat meta.__vaultified.haproxy_ssl+certificate ... )).
		// Spruce lexes the whole whitespace-free token as one reference;
		// the '+' must not be read as addition.
		got := passthroughGet(t, `meta:
  __vaultified:
    haproxy_ssl+certificate: CERT
    haproxy_ssl+private_key: KEY
x: (( concat meta.__vaultified.haproxy_ssl+certificate meta.__vaultified.haproxy_ssl+private_key ))
`)
		if got != "CERTKEY" {
			t.Fatalf("expected CERTKEY, got %v (%T)", got, got)
		}
	})

	t.Run("tight plus before a digit is still addition", func(t *testing.T) {
		got := passthroughGet(t, "meta:\n  count: 4\nx: (( meta.count+1 ))\n")
		if got != int64(5) {
			t.Fatalf("expected 5, got %v (%T)", got, got)
		}
	})

	t.Run("placeholders under a list survive verbatim", func(t *testing.T) {
		doc, err := mergeYAML(t, "certs:\n- ((diego_instance_identity_ca.ca))\n- ((uaa_ssl.ca))\n")
		if err != nil {
			t.Fatalf("unexpected merge error: %v", err)
		}
		got, err := doc.Get("certs.0")
		if err != nil {
			t.Fatalf("failed to read certs.0: %v", err)
		}
		if got != "((diego_instance_identity_ca.ca))" {
			t.Fatalf("expected verbatim placeholder in list, got %v (%T)", got, got)
		}
	})
}
