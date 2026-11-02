package yaml_test

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// utf16Bytes encodes text as UTF-16 with a byte order mark.
func utf16Bytes(text string, order binary.ByteOrder) []byte {
	units := append([]uint16{0xFEFF}, utf16.Encode([]rune(text))...)
	out := make([]byte, 2*len(units))
	for i, u := range units {
		order.PutUint16(out[2*i:], u)
	}
	return out
}

const utf16Doc = "name: café\nitems: [x, \U0001F600]\nflag: yes\n# end\n"

func TestUnmarshalReadsUTF16WithBOM(t *testing.T) {
	t.Parallel()
	var want any
	if err := yaml.Unmarshal([]byte(utf16Doc), &want); err != nil {
		t.Fatal(err)
	}
	orders := map[string]binary.ByteOrder{"little endian": binary.LittleEndian, "big endian": binary.BigEndian}
	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := utf16Bytes(utf16Doc, order)

			var got any
			if err := yaml.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Unmarshal gave %#v, want %#v", got, want)
			}

			var node yaml.Node
			if err := yaml.Unmarshal(data, &node); err != nil {
				t.Fatalf("Unmarshal into a Node: %v", err)
			}
			var viaNode any
			if err := node.Decode(&viaNode); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !reflect.DeepEqual(viaNode, got) {
				t.Errorf("Decode gave %#v, Unmarshal gave %#v", viaNode, got)
			}

			var typed struct {
				Name  string   `yaml:"name"`
				Items []string `yaml:"items"`
				Flag  bool     `yaml:"flag"`
			}
			if err := yaml.Unmarshal(data, &typed); err != nil {
				t.Fatalf("Unmarshal into a struct: %v", err)
			}
			if typed.Name != "café" || len(typed.Items) != 2 || typed.Items[1] != "\U0001F600" || !typed.Flag {
				t.Errorf("struct got %+v", typed)
			}
		})
	}
}

func TestUnmarshalRefusesBrokenUTF16(t *testing.T) {
	t.Parallel()
	secret := "S3CR3Tvalue"
	good := utf16Bytes("k: "+secret+"\n", binary.LittleEndian)
	unpaired := utf16Bytes("k: "+secret+"\n", binary.LittleEndian)
	// Replace the last two characters before the newline with a lone high surrogate.
	unpaired[len(unpaired)-4], unpaired[len(unpaired)-3] = 0x00, 0xD8
	cases := map[string][]byte{
		"lone BOM little endian": {0xFF, 0xFE},
		"lone BOM big endian":    {0xFE, 0xFF},
		"odd byte count":         good[:len(good)-1],
		"unpaired surrogate":     unpaired,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var got any
			err := yaml.Unmarshal(data, &got)
			de := wantDecodeError(t, err, yaml.ErrorKindSyntax, 0)
			if de != nil && strings.Contains(err.Error(), secret) {
				t.Errorf("error quotes the input: %v", err)
			}
			var node yaml.Node
			err = yaml.Unmarshal(data, &node)
			wantDecodeError(t, err, yaml.ErrorKindSyntax, 0)
		})
	}
}
