package natsbackend

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/errtext"
)

// connectPassword is the password in the URLs these tests fail to
// connect with. No Error() text and no detailed text may hold it.
const connectPassword = "pwzq81x"

// assertNoPassword checks that neither of err's texts, nor the text of
// any error on its Unwrap chain, holds the password.
func assertNoPassword(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("the call must fail")
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.Contains(e.Error(), connectPassword) {
			t.Errorf("a %T on the chain quotes the password:\n%s", e, e.Error())
		}
	}
	if detail := errtext.Detail(err); strings.Contains(detail, connectPassword) {
		t.Errorf("the detailed text quotes the password:\n%s", detail)
	}
}

// TestConnectErrorDoesNotQuoteTheURL makes the connection fail on a URL
// that does not parse. Error() names neither the URL nor its password,
// and the detailed text names the URL with its credentials redacted.
func TestConnectErrorDoesNotQuoteTheURL(t *testing.T) {
	urls := []string{
		"nats://user:" + connectPassword + "@[bad",
		"nats://user:" + connectPassword + "@host:badport",
		"nats://user:" + connectPassword + "/x@host:4222",
		"nats://" + connectPassword + "@host:badport",
	}
	connect := map[string]func(*Config) error{
		"CreateConnectionWithRetry": func(c *Config) error {
			_, _, err := CreateConnectionWithRetry(c)
			return err
		},
		"CreateConnectionFromConfig": func(c *Config) error {
			_, err := CreateConnectionFromConfig(c)
			return err
		},
	}
	for name, fn := range connect {
		for _, u := range urls {
			t.Run(name+"/"+u, func(t *testing.T) {
				err := fn(&Config{URL: u})
				assertNoPassword(t, err)
				if !strings.HasPrefix(err.Error(), "failed to connect to NATS after 1 attempts") {
					t.Errorf("Error() is %q", err.Error())
				}
				if strings.Contains(err.Error(), "host") || strings.Contains(err.Error(), "user") {
					t.Errorf("Error() quotes the URL: %q", err.Error())
				}
				if detail := errtext.Detail(err); !strings.Contains(detail, "xxxxx@") {
					t.Errorf("the detailed text lost the redacted URL:\n%s", detail)
				}
			})
		}
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"nats://localhost:4222", "nats://localhost:4222"},
		{"nats://user:pw@host:4222", "nats://xxxxx@host:4222"},
		{"nats://token@host:4222", "nats://xxxxx@host:4222"},
		{"user:pw@host:4222", "xxxxx@host:4222"},
		{"nats://u:p@w@host", "nats://xxxxx@host"},
		{"nats://u:p://x@host", "nats://xxxxx@host"},
		{"u:p://x@host", "xxxxx@host"},
		{"nats://a:p@h1,nats://b:q@h2", "nats://xxxxx@h2"},
		{"nats://h1,nats://h2", "nats://h1,nats://h2"},
	}
	for _, tc := range tests {
		if got := redactURL(tc.in); got != tc.want {
			t.Errorf("redactURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
