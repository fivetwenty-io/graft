package yamldiff

import "testing"

// TestToBoolPinsTheYAML11Set pins the boolean literals dyff accepted, which
// are the YAML 1.1 forms, so a later change cannot narrow them to the YAML
// 1.2 core forms that internal/yamlnode resolves.
func TestToBoolPinsTheYAML11Set(t *testing.T) {
	tests := []struct {
		input   string
		want    bool
		wantErr bool
	}{
		{input: "y", want: true},
		{input: "Y", want: true},
		{input: "yes", want: true},
		{input: "Yes", want: true},
		{input: "YES", want: true},
		{input: "true", want: true},
		{input: "True", want: true},
		{input: "TRUE", want: true},
		{input: "on", want: true},
		{input: "On", want: true},
		{input: "ON", want: true},
		{input: "n", want: false},
		{input: "N", want: false},
		{input: "no", want: false},
		{input: "No", want: false},
		{input: "NO", want: false},
		{input: "false", want: false},
		{input: "False", want: false},
		{input: "FALSE", want: false},
		{input: "off", want: false},
		{input: "Off", want: false},
		{input: "OFF", want: false},
		{input: "maybe", wantErr: true},
		{input: "", wantErr: true},
		{input: "tRuE", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := toBool(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("toBool(%q) = %v, want an error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("toBool(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("toBool(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
