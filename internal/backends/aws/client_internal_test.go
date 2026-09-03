package aws

import "testing"

// TestEndpointFor covers endpointFor's whole documented contract: an
// empty Endpoint returns "", a scheme-less Endpoint gets "http://" or
// "https://" prepended depending on DisableSSL (mirroring v1's
// endpoints.AddScheme, which ran ahead of every custom endpoint), an
// "https://" Endpoint with DisableSSL set is rewritten to "http://", and
// every other combination (an already-scheme'd Endpoint whose scheme
// already matches DisableSSL, or an "http://" Endpoint regardless of
// DisableSSL) is returned verbatim.
func TestEndpointFor(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		disableSSL bool
		want       string
	}{
		{
			name:       "empty endpoint returns empty",
			endpoint:   "",
			disableSSL: false,
			want:       "",
		},
		{
			name:       "empty endpoint returns empty even with DisableSSL",
			endpoint:   "",
			disableSSL: true,
			want:       "",
		},
		{
			name:       "scheme-less endpoint gets https when DisableSSL is false",
			endpoint:   "localhost:4566",
			disableSSL: false,
			want:       "https://localhost:4566",
		},
		{
			name:       "scheme-less endpoint gets http when DisableSSL is true",
			endpoint:   "localhost:4566",
			disableSSL: true,
			want:       "http://localhost:4566",
		},
		{
			name:       "https endpoint is rewritten to http when DisableSSL is true",
			endpoint:   "https://localhost:4566",
			disableSSL: true,
			want:       "http://localhost:4566",
		},
		{
			name:       "http endpoint is unchanged regardless of DisableSSL",
			endpoint:   "http://localhost:4566",
			disableSSL: false,
			want:       "http://localhost:4566",
		},
		{
			name:       "http endpoint is unchanged even when DisableSSL is true",
			endpoint:   "http://localhost:4566",
			disableSSL: true,
			want:       "http://localhost:4566",
		},
		{
			name:       "https endpoint is unchanged when DisableSSL is false",
			endpoint:   "https://localhost:4566",
			disableSSL: false,
			want:       "https://localhost:4566",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &Target{Endpoint: tt.endpoint, DisableSSL: tt.disableSSL}
			if got := endpointFor(target); got != tt.want {
				t.Errorf("endpointFor(%+v) = %q, want %q", target, got, tt.want)
			}
		})
	}
}
