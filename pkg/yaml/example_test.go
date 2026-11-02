package yaml_test

import (
	"errors"
	"fmt"

	"github.com/fivetwenty-io/graft/pkg/sortkeys"
	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func ExampleUnmarshal() {
	type Config struct {
		Name    string   `yaml:"name"`
		Workers int      `yaml:"workers"`
		Debug   bool     `yaml:"debug"`
		Hosts   []string `yaml:"hosts"`
	}

	data := []byte("name: api\nworkers: 4\ndebug: yes\nhosts: [a, b]\n")
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("%s %d %v %v\n", cfg.Name, cfg.Workers, cfg.Debug, cfg.Hosts)
	// Output: api 4 true [a b]
}

func ExampleUnmarshalWith() {
	data := []byte("answer: yes\n")

	var v11, v12 map[string]any
	if err := yaml.Unmarshal(data, &v11); err != nil {
		fmt.Println("error:", err)
		return
	}
	if err := yaml.UnmarshalWith(data, &v12, yaml.DecodeOptions{YAML12: true}); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("%#v\n%#v\n", v11["answer"], v12["answer"])
	// Output:
	// true
	// "yes"
}

func ExampleMarshal() {
	out, err := yaml.Marshal(map[string]any{
		"name":  "api",
		"ports": []int{80, 443},
		"debug": false,
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Print(string(out))
	// Output:
	// debug: false
	// name: api
	// ports:
	// - 80
	// - 443
}

func ExampleMarshalWith() {
	value := map[string]any{"10": "ten", "9": "nine", "a": "letter"}

	spruce, _ := yaml.Marshal(value)
	lexical, _ := yaml.MarshalWith(value, yaml.EncodeOptions{KeyOrder: sortkeys.Lexical})
	fmt.Printf("spruce order:\n%slexical order:\n%s", spruce, lexical)
	// Output:
	// spruce order:
	// "9": nine
	// "10": ten
	// a: letter
	// lexical order:
	// "10": ten
	// "9": nine
	// a: letter
}

func ExampleDecodeError() {
	var m map[string]int
	err := yaml.Unmarshal([]byte("count: not-a-number\n"), &m)

	var de *yaml.DecodeError
	if errors.As(err, &de) {
		fmt.Println(de.Line, de.Kind == yaml.ErrorKindType)
		fmt.Println(de.Error())
	}
	// Output:
	// 1 true
	// yaml: line 1: cannot unmarshal string into Go value of type int
}

func ExampleNode() {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("name: api\nports: [80, 443]\n"), &doc); err != nil {
		fmt.Println("error:", err)
		return
	}

	root := doc.Content[0]
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		fmt.Printf("%s is on line %d and holds a %s\n", key.Value, key.Line, value.Tag)
	}

	var ports []int
	if err := root.Content[3].Decode(&ports); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(ports)
	// Output:
	// name is on line 1 and holds a !!str
	// ports is on line 2 and holds a !!seq
	// [80 443]
}
