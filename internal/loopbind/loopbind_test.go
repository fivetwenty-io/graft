package loopbind

import (
	"fmt"
	"testing"
)

const bound = "__graft_loop.l0123456789ab_1.0."

func TestWithoutText(t *testing.T) {
	data := map[string]interface{}{
		"(( " + bound + "svc ))": 1,
		"(( svc ))":              2,
		"list":                   []interface{}{"big-(( grab " + bound + "i ))", 3},
	}
	WithoutText(data)
	got := fmt.Sprint(data)
	want := "map[(( " + bound + "svc )):1 (( svc )):2 list:[big-(( grab i )) 3]]"
	if got != want {
		t.Errorf("WithoutText = %s, want %s", got, want)
	}
}

func TestStrip(t *testing.T) {
	doc := map[string]interface{}{Key: map[string]interface{}{}, "out": "(( grab " + bound + "svc.n ))"}
	Strip(doc)
	if got := fmt.Sprint(doc); got != "map[out:(( grab svc.n ))]" {
		t.Errorf("Strip = %s", got)
	}
	plain := map[string]interface{}{"out": bound + "x"}
	Strip(plain)
	if plain["out"] != bound+"x" {
		t.Errorf("Strip changed a document with no binding tree: %v", plain)
	}
}
