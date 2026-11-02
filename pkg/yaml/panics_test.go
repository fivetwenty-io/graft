package yaml

import "testing"

// TestInDecoder checks that only goccy's module and the standard
// library's reflect count as the decoder, and look-alike paths do not.
func TestInDecoder(t *testing.T) {
	cases := map[string]bool{
		"github.com/goccy/go-yaml.(*Decoder).decodeStruct":   true,
		"github.com/goccy/go-yaml/ast.(*MappingNode).String": true,
		"reflect.Value.Field":                                true,
		"reflect.(*rtype).Method":                            true,
		"github.com/goccy/go-yamlx.Decode":                   false,
		"github.com/goccy/go-yaml-fork.Decode":               false,
		"github.com/goccy/go-yaml2/ast.Parse":                false,
		"example.com/reflect.Value":                          false,
		"reflect.Value.Call.func1/example.com/x":             false,
		"github.com/fivetwenty-io/graft/pkg/yaml_test.f":     false,
	}
	for function, want := range cases {
		if got := inDecoder(function); got != want {
			t.Errorf("inDecoder(%q) = %v, want %v", function, got, want)
		}
	}
}

// TestInEncoder checks that graft's own encoder counts with the decoder's
// packages as the encoder, and look-alike paths do not.
func TestInEncoder(t *testing.T) {
	cases := map[string]bool{
		"github.com/fivetwenty-io/graft/internal/yamlcodec.(*scalarEncoder).prepare": true,
		"github.com/fivetwenty-io/graft/internal/yamlcodec.MarshalYAMLTyped":         true,
		"github.com/goccy/go-yaml.(*Encoder).encodeValue":                            true,
		"reflect.Value.Field": true,
		"github.com/fivetwenty-io/graft/internal/yamlcodecx.Marshal":  false,
		"github.com/fivetwenty-io/graft/internal/yamlcodec/x.Marshal": false,
		"github.com/fivetwenty-io/graft/pkg/yaml_test.f":              false,
	}
	for function, want := range cases {
		if got := inEncoder(function); got != want {
			t.Errorf("inEncoder(%q) = %v, want %v", function, got, want)
		}
	}
}
