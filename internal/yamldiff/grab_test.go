package yamldiff

import "testing"

func TestK8sNameScalarMetadataQuirk(t *testing.T) {
	for src, want := range map[string]string{
		"apiVersion: v1\nkind: X\nmetadata: foo\nv: 1\n":                     "v1/X/foo/foo",
		"apiVersion: v1\nkind: Secret\nmetadata: {name: c, namespace: ns}\n": "v1/Secret/ns/c",
		"apiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\n":             "v1/ConfigMap/a",
		"m: &m bar\napiVersion: v1\nkind: X\nmetadata: *m\n":                 "v1/X/m/m",
	} {
		got, err := k8sItem.Name(mustParse(t, src)[0].Content[0])
		if err != nil || got != want {
			t.Errorf("k8sItem.Name(%q) = %q, %v; want %q", src, got, err, want)
		}
	}
	for _, src := range []string{"apiVersion: v1\nmetadata: {name: a}\n", "kind: X\nmetadata: {name: a}\n", "apiVersion: v1\nkind: X\n"} {
		if _, err := k8sItem.Name(mustParse(t, src)[0].Content[0]); err == nil {
			t.Errorf("k8sItem.Name(%q) must fail", src)
		}
	}
	if k8sItem.String() != "resource" {
		t.Errorf("k8sItem.String() = %q, want resource", k8sItem.String())
	}
}

func TestSingleFieldFollowsAliasValues(t *testing.T) {
	m := mustParse(t, "x: &n web\nentry: {name: *n}\n")[0].Content[0].Content[3]
	got, err := (&singleField{IdentifierFieldName: "name"}).Name(m)
	if err != nil || got != "web" {
		t.Fatalf("singleField.Name = %q, %v; want web", got, err)
	}
}
