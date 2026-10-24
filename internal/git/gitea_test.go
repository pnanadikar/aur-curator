package git

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateRepository(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatal("method")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"clone_url":"https://gitea.example.com/aur/yay.git"}`)
	}))
	defer ts.Close()
	f := NewGiteaForge(ts.URL, "aur", "tok", "test")
	url, err := f.CreateRepository(context.Background(), "yay")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://gitea.example.com/aur/yay.git" {
		t.Fatalf("expected clone URL, got %q", url)
	}
}

func TestCreateRepositoryMissingCloneURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatal("method")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"id":1,"name":"yay"}`)
	}))
	defer ts.Close()
	f := NewGiteaForge(ts.URL, "aur", "tok", "test")
	_, err := f.CreateRepository(context.Background(), "yay")
	if err == nil {
		t.Fatal("expected error for missing clone_url")
	}
}
