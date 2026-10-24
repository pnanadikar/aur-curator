package git

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGiteaStatusErrors400: 401/403 say why and where the token came
// from; every status carries Gitea's own message when the body has one;
// the token never appears (#400).
func TestGiteaStatusErrors400(t *testing.T) {
	const src = "AUR_CURATOR_GITEA_TOKEN (environment)"
	const scopeMsg = `{"message":"token does not have at least one of required scope(s): [write:organization]"}`
	cases := []struct {
		name   string
		delete bool
		status int
		body   string
		want   string
	}{
		{name: "create 401", status: 401, body: `{"message":"user does not exist [uid: 0, name: ]"}`,
			want: `create repository: Gitea rejected the API token (401 Unauthorized): invalid, expired or revoked; Gitea: "user does not exist [uid: 0, name: ]"; token from ` + src},
		{name: "create 401 no body", status: 401,
			want: `create repository: Gitea rejected the API token (401 Unauthorized): invalid, expired or revoked; token from ` + src},
		{name: "create 403", status: 403, body: scopeMsg,
			want: `create repository: the API token is not allowed to create repositories in "aur" (403 Forbidden): it needs the write:organization scope and that permission in the organization; Gitea: "token does not have at least one of required scope(s): [write:organization]"; token from ` + src},
		{name: "create 409", status: 409, body: `{"message":"The repository with the same name already exists."}`,
			want: `create repository: unexpected status 409 (repository with the same name already exists); Gitea: "The repository with the same name already exists."`},
		{name: "create 500 non-JSON", status: 500, body: "<html>oops</html>",
			want: `create repository: unexpected status 500`},
		{name: "delete 401", delete: true, status: 401,
			want: `delete repository: Gitea rejected the API token (401 Unauthorized): invalid, expired or revoked; token from ` + src},
		{name: "delete 403", delete: true, status: 403, body: `{"message":"token does not have at least one of required scope(s): [write:repository]"}`,
			want: `delete repository: the API token is not allowed to delete repositories in "aur" (403 Forbidden): it needs the write:repository scope and that permission in the organization; Gitea: "token does not have at least one of required scope(s): [write:repository]"; token from ` + src},
		{name: "delete 500", delete: true, status: 500, body: `{"message":"internal\nerror"}`,
			want: `delete repository: unexpected status 500; Gitea: "internal error"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer ts.Close()
			f := NewGiteaForge(ts.URL, "aur", "s3cret-token", src)

			var err error
			op := ErrCreateRepository
			if tc.delete {
				op = ErrDeleteRepository
				err = f.DeleteRepository(context.Background(), "yay")
			} else {
				_, err = f.CreateRepository(context.Background(), "yay")
			}

			if err == nil {
				t.Fatal("err = nil, want an error")
			}
			if err.Error() != tc.want {
				t.Fatalf("err = %s\nwant  %s", err.Error(), tc.want)
			}
			if !errors.Is(err, op) {
				t.Fatalf("err does not wrap %v", op)
			}
			if strings.Contains(err.Error(), "s3cret-token") {
				t.Fatal("error contains the token")
			}
		})
	}
}
