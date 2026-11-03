package graft_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestLiteralOperatorErrorsDoNotQuoteContent makes operators fail on text
// read from a wrapped value. The error names the path, and no message
// shows the text.
func TestLiteralOperatorErrorsDoNotQuoteContent(t *testing.T) {
	const secret = "s3cr3t-do-not-print"
	params := map[string]interface{}{
		"s":     secret,
		"zones": []interface{}{secret},
		"num":   map[string]interface{}{"x": secret},
		"bad":   []interface{}{4242},
	}
	tests := []struct {
		name string
		plan string
	}{
		{name: "calc", plan: "out: (( calc params.s ))\n"},
		{name: "ips", plan: "out: (( ips params.s 1 ))\n"},
		{name: "base64-decode", plan: "out: (( base64-decode params.s ))\n"},
		{name: "load", plan: "out: (( load params.s ))\n"},
		{name: "file", plan: "out: (( file params.s ))\n"},
		{name: "grab through a scalar", plan: "out: (( grab params.s.deeper ))\n"},
		{name: "grab of a path built from the literal", plan: "out: (( grab (concat \"meta.\" params.s) ))\n"},
		{name: "static_ips azs not strings", plan: `
networks:
  - name: net1
    subnets:
      - static: [10.0.0.1 - 10.0.0.5]
        azs: [z1]
instance_groups:
  - name: web
    instances: 1
    azs: (( grab params.bad ))
    networks:
      - name: net1
        static_ips: (( static_ips 0 ))
`},
		{name: "static_ips", plan: `
networks:
  - name: net1
    subnets:
      - static: [10.0.0.1 - 10.0.0.5]
        azs: [z1]
instance_groups:
  - name: web
    instances: 1
    azs: (( grab params.zones ))
    networks:
      - name: net1
        static_ips: (( static_ips 0 ))
`},
	}
	for _, mode := range leakModes {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				eng, _ := leakEngine(t, mode.parallel)
				planDoc, err := eng.ParseYAML([]byte(tt.plan))
				if err != nil {
					t.Fatal(err)
				}
				user := graft.NewDocument(map[string]interface{}{"params": graft.NewLiteral(params)})
				_, err = eng.Merge(context.Background(), planDoc, user).Execute()
				if err == nil {
					t.Fatal("Execute succeeded on input that must fail")
				}
				if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "literal.Value") || strings.Contains(err.Error(), "4242") {
					t.Fatalf("error quotes literal content: %v", err)
				}
				var pathErr *graft.PathError
				if !errors.As(err, &pathErr) && !strings.Contains(err.Error(), "$.") {
					t.Fatalf("error does not name a path: %v", err)
				}
			})
		}
	}
}

// TestWithheldLiteralErrorKeepsItsDetail checks that withholding an
// error's message from Error() leaves the detailed text alone, so a
// caller that asks graft.DetailedError for it still gets the message
// the operator wrote.
func TestWithheldLiteralErrorKeepsItsDetail(t *testing.T) {
	const secret = "s3cr3t-do-not-print"
	params := map[string]interface{}{"s": secret}
	for _, mode := range leakModes {
		t.Run(mode.name, func(t *testing.T) {
			eng, _ := leakEngine(t, mode.parallel)
			planDoc, err := eng.ParseYAML([]byte("out: (( grab (concat \"meta.\" params.s) ))\n"))
			if err != nil {
				t.Fatal(err)
			}
			user := graft.NewDocument(map[string]interface{}{"params": graft.NewLiteral(params)})
			_, err = eng.Merge(context.Background(), planDoc, user).Execute()
			if err == nil {
				t.Fatal("Execute succeeded on input that must fail")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("Error() quotes literal content: %v", err)
			}
			assertChainPlain(t, err, secret)
			detail := graft.DetailedError(err)
			if !strings.Contains(detail, "unable to resolve `meta."+secret+"`") {
				t.Fatalf("DetailedError lost the operator's message: %q", detail)
			}
		})
	}
}
