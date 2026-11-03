package operators

import (
	"context"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestNatsConnectErrorHoldsNoPassword fails a (( nats )) lookup on a URL
// with a password that does not parse. Neither Error() nor the detailed
// text holds the password, and Error() holds no part of the URL.
func TestNatsConnectErrorHoldsNoPassword(t *testing.T) {
	const password = "pwzq81x"
	urls := []string{
		"nats://user:" + password + "@[bad",
		"nats://user:" + password + "/x@host:4222",
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			engine, err := graft.NewEngine()
			if err != nil {
				t.Fatal(err)
			}
			doc := graft.NewDocument(map[string]interface{}{
				"meta": map[string]interface{}{"nats": map[string]interface{}{"url": u, "retries": 0}},
				"out":  `(( nats "kv:store/key" meta.nats ))`,
			})
			_, err = engine.Evaluate(context.Background(), doc)
			if err == nil {
				t.Fatal("the lookup must fail")
			}
			if strings.Contains(err.Error(), password) || strings.Contains(err.Error(), "host") {
				t.Errorf("Error() quotes the URL:\n%s", err.Error())
			}
			if !strings.Contains(err.Error(), "failed to connect to NATS") {
				t.Errorf("Error() lost the reason:\n%s", err.Error())
			}
			if detail := graft.DetailedError(err); strings.Contains(detail, password) {
				t.Errorf("the detailed text quotes the password:\n%s", detail)
			}
		})
	}
}
