package graft_test

import (
	"context"
	"testing"
)

// mergeErrorCase is a pair of documents whose merge fails. base and
// overlay hold the probe where the case plants it.
type mergeErrorCase struct {
	name     string
	base     string
	overlay  string
	probe    string
	detailed bool
}

var mergeErrorCases = []mergeErrorCase{
	{
		name:     "delete with an orphan entry",
		base:     "list: [a, PROBEzq81x]\n",
		overlay:  "list:\n  - (( delete \"PROBEzq81x\" ))\n  - x\n",
		detailed: true,
	},
	{
		name:     "insert after a missing entry",
		base:     "list: [a, b]\n",
		overlay:  "list:\n  - (( insert after \"PROBEzq81x\" ))\n  - c\n",
		detailed: true,
	},
	{
		name:     "keyed insert into a list of scalars",
		base:     "list: [a, b]\n",
		overlay:  "list:\n  - (( insert after name \"PROBEzq81x\" ))\n  - c\n",
		detailed: true,
	},
	{
		name:     "keyed delete with an orphan entry",
		base:     "list: [{name: a}]\n",
		overlay:  "list:\n  - (( delete name \"PROBEzq81x\" ))\n  - {name: c}\n",
		detailed: true,
	},
	{
		name:     "insert of an entry that already exists",
		base:     "list: [{name: PROBEzq81x}]\n",
		overlay:  "list:\n  - (( insert after name \"PROBEzq81x\" ))\n  - {name: PROBEzq81x}\n",
		detailed: true,
	},
	{
		name:     "keyed insert after a missing entry",
		base:     "list: [{name: a}]\n",
		overlay:  "list:\n  - (( insert after name \"PROBEzq81x\" ))\n  - {name: c}\n",
		detailed: true,
	},
	{
		name:     "insert past the end",
		base:     "list: [a]\n",
		overlay:  "list:\n  - (( insert after 4079 ))\n  - c\n",
		probe:    "4080",
		detailed: true,
	},
	{
		name:    "delete outside a list",
		base:    "x: {k: PROBEzq81x}\n",
		overlay: "x: (( delete \"PROBEzq81x\" ))\n",
	},
	{
		name:    "merge on a key into a list of scalars",
		base:    "list: [{name: a}]\n",
		overlay: "list:\n  - (( merge on name ))\n  - PROBEzq81x\n",
	},
	{
		name:    "merge on a key an entry lacks",
		base:    "list: [{name: a}]\n",
		overlay: "list:\n  - (( merge on name ))\n  - {other: PROBEzq81x}\n",
	},
	{
		name:    "merge on a key whose value is a map",
		base:    "list: [{name: a}]\n",
		overlay: "list:\n  - (( merge on name ))\n  - {name: {k: PROBEzq81x}}\n",
	},
}

// TestMergeErrorTextHoldsNoValueOrColor fails a merge for each list
// operator and checks the error's two texts with the ansi toggle on.
func TestMergeErrorTextHoldsNoValueOrColor(t *testing.T) {
	withColorOn(t)
	for _, tc := range mergeErrorCases {
		t.Run(tc.name, func(t *testing.T) {
			eng, _ := leakEngine(t, false)
			base, err := eng.ParseYAML([]byte(tc.base))
			if err != nil {
				t.Fatalf("base: %v", err)
			}
			overlay, err := eng.ParseYAML([]byte(tc.overlay))
			if err != nil {
				t.Fatalf("overlay: %v", err)
			}
			_, err = eng.Merge(context.Background(), base, overlay).Execute()
			probe := tc.probe
			if probe == "" {
				probe = errProbe
			}
			assertErrorText(t, err, probe, tc.detailed)
		})
	}
}
