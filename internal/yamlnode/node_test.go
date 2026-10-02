package yamlnode

import "testing"

func TestFollowAliasWalksTheWholeChain(t *testing.T) {
	target := &Node{Kind: MappingNode, Tag: "!!map", Anchor: "a"}
	first := &Node{Kind: AliasNode, Value: "a", Alias: target}
	second := &Node{Kind: AliasNode, Value: "a", Alias: first}
	if got := FollowAlias(second); got != target {
		t.Fatalf("FollowAlias returned %+v, want the anchored mapping", got)
	}
	if got := FollowAlias(target); got != target {
		t.Fatal("FollowAlias must return a non-alias node unchanged")
	}
	if FollowAlias(nil) != nil {
		t.Fatal("FollowAlias(nil) must be nil")
	}
}
