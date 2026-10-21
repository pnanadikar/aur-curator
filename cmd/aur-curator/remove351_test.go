package main

import (
	"errors"
	"os/exec"
	"testing"
)

// #351 — Remove never uninstalls; it only checks with pacman -Q.

func exitErr(t *testing.T, code string) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+code).Run()
	if err == nil {
		t.Fatal("setup: expected a non-zero exit")
	}
	return err
}

func TestInterpretPacmanQ(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		err     error
		want    string
		wantErr bool
	}{
		{"installed", "yay 12.5.0-1\n", nil, "12.5.0-1", false},
		{"not installed (exit 1)", "", exitErr(t, "1"), "", false},
		{"other exit code is an error", "", exitErr(t, "2"), "", true},
		{"pacman missing is an error", "", exec.ErrNotFound, "", true},
		{"other error is an error", "", errors.New("boom"), "", true},
		{"unexpected output is an error", "garbage", nil, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := interpretPacmanQ([]byte(tc.out), tc.err)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("version = %q, want %q", got, tc.want)
			}
		})
	}
}
