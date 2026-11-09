package config

import (
	"strings"
	"testing"
)

// TestGiteaBaseURLPlainHTTP: http:// is accepted only when the host is this
// machine, because every Gitea API request carries the token (#505).
func TestGiteaBaseURLPlainHTTP(t *testing.T) {
	tests := []struct {
		url    string
		wantOK bool
	}{
		{"https://gitea.example.com", true},
		{"https://192.168.1.10:3000", true},
		{"http://localhost", true},
		{"http://localhost:3000", true},
		{"http://127.0.0.1:3000", true},
		{"http://127.1.2.3", true},
		{"http://[::1]:3000", true},
		{"http://gitea.example.com", false},
		{"http://192.168.1.10:3000", false},
		{"http://[fe80::1]", false},
		{"http://localhost.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			r := load408(t, map[string]string{"giteaBaseURL": `"` + tt.url + `"`})
			if tt.wantOK {
				if r.err != nil {
					t.Fatalf("Load error = %v, want accepted", r.err)
				}
				return
			}
			if r.err == nil {
				t.Fatalf("Load accepted %s, want refused", tt.url)
			}
			for _, want := range []string{"giteaBaseURL", tt.url, "unencrypted", "https://"} {
				if !strings.Contains(r.err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", r.err, want)
				}
			}
		})
	}
}
