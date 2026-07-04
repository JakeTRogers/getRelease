package github

import "testing"

func TestNormalizeHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		host    string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to github.com", host: "", want: "github.com"},
		{name: "github.com", host: "github.com", want: "github.com"},
		{name: "www.github.com", host: "www.github.com", want: "github.com"},
		{name: "case insensitive", host: "GitHub.Com", want: "github.com"},
		{name: "whitespace trimmed", host: "  github.com  ", want: "github.com"},
		{name: "ghe.com host", host: "acme.ghe.com", want: "acme.ghe.com"},
		{name: "ghe.com host lowercased", host: "Acme.Ghe.Com", want: "acme.ghe.com"},
		{name: "self-hosted GHES domain rejected", host: "github.acme.internal", wantErr: true},
		{name: "unrelated domain rejected", host: "gitlab.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeHost(tt.host)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeHost(%q) error = %v, wantErr %v", tt.host, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Errorf("NormalizeHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}
