package aurinfo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// lookupServer answers every request with body and status, and records
// the request's query.
func lookupServer(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var query string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts, &query
}

// TestLookup covers the record fields and how orphaned, absent
// co-maintainers and a missing package are told apart (#495). The
// bodies follow aurweb's info output: Maintainer always present, null
// when orphaned; CoMaintainers only when there is at least one.
func TestLookup(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Record
	}{
		{
			name: "maintained with co-maintainers",
			body: `{"resultcount":1,"type":"multiinfo","version":5,"results":[{"Name":"foo","Version":"1-1",
				"Maintainer":"alice","CoMaintainers":["carol","bob"],"Submitter":"sam",
				"FirstSubmitted":1700000000,"NumVotes":42,"Popularity":1.234567}]}`,
			want: Record{
				Maintainer:     "alice",
				CoMaintainers:  []string{"bob", "carol"},
				Submitter:      "sam",
				FirstSubmitted: time.Unix(1700000000, 0).UTC(),
				NumVotes:       42,
				Popularity:     1.234567,
			},
		},
		{
			name: "orphaned",
			body: `{"resultcount":1,"type":"multiinfo","version":5,"results":[{"Name":"foo","Version":"1-1",
				"Maintainer":null,"Submitter":"sam","FirstSubmitted":1700000000,"NumVotes":0,"Popularity":0}]}`,
			want: Record{
				Orphaned:       true,
				Submitter:      "sam",
				FirstSubmitted: time.Unix(1700000000, 0).UTC(),
			},
		},
		{
			name: "no co-maintainers key",
			body: `{"resultcount":1,"type":"multiinfo","version":5,"results":[{"Name":"foo","Version":"1-1",
				"Maintainer":"alice","Submitter":"alice","FirstSubmitted":0,"NumVotes":1,"Popularity":0.5}]}`,
			want: Record{
				Maintainer: "alice",
				Submitter:  "alice",
				NumVotes:   1,
				Popularity: 0.5,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, query := lookupServer(t, http.StatusOK, tt.body)
			client := NewClient(ts.URL, 5*time.Second, nil)

			got, err := client.Lookup(context.Background(), "foo")
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Lookup = %+v, want %+v", got, tt.want)
			}
			if *query != "v=5&type=info&arg[]=foo" {
				t.Errorf("query = %q", *query)
			}
		})
	}
}

// TestLookup_Errors: a missing package and every failed request are
// errors, distinguishable by their sentinel; none is a Record.
func TestLookup_Errors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"not found", http.StatusOK, `{"resultcount":0,"type":"multiinfo","version":5,"results":[]}`, ErrAURPackageNotFound},
		{"other name only", http.StatusOK, `{"resultcount":1,"type":"multiinfo","version":5,"results":[{"Name":"foo-git","Maintainer":"a"}]}`, ErrAURPackageNotFound},
		{"HTTP 500", http.StatusInternalServerError, `oops`, ErrAURClientUnreachable},
		{"malformed", http.StatusOK, `{not json`, ErrAURClientUnreachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, _ := lookupServer(t, tt.status, tt.body)
			client := NewClient(ts.URL, 5*time.Second, nil)

			got, err := client.Lookup(context.Background(), "foo")
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(got, Record{}) {
				t.Errorf("Record = %+v, want zero", got)
			}
		})
	}
}

// TestLookup_ClientTimeout: the client's own timeout bounds Lookup, as it
// does FetchAURVersions (#416).
func TestLookup_ClientTimeout(t *testing.T) {
	client := NewClient("http://aur.invalid/rpc/", 50*time.Millisecond, nil)
	client.client = blockingDoer{}

	done := make(chan error, 1)
	go func() {
		_, err := client.Lookup(context.Background(), "foo")
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrAURClientUnreachable) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want ErrAURClientUnreachable and DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Lookup did not return: client timeout not applied")
	}
}
