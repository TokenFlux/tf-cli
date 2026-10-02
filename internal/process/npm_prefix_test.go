package process

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if prefix := os.Getenv("TF_TEST_NPM_PREFIX"); prefix != "" {
		if prefix == "sleep" {
			time.Sleep(time.Minute)
		}
		fmt.Println(prefix)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNpmPrefixRedirect(t *testing.T) {
	for _, scenario := range []string{"local", "upgraded", "invalid-prefix", "cancelled", "no-helper"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			entry := filepath.Join(root, "bundled", "node_modules", "npm", "bin", "npm-cli.js")
			global := filepath.Join(root, "global")
			candidate := filepath.Join(global, "node_modules", "npm", "bin", "npm-cli.js")
			if err := os.MkdirAll(filepath.Dir(entry), 0700); err != nil {
				t.Fatal(err)
			}
			if scenario != "no-helper" {
				if err := os.WriteFile(filepath.Join(filepath.Dir(entry), "npm-prefix.js"), []byte("fixture helper"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "upgraded" {
				if err := os.MkdirAll(filepath.Dir(candidate), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(candidate, []byte("fixture global npm"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			prefix := global
			if scenario == "invalid-prefix" {
				prefix = "relative-path"
			}
			if scenario == "cancelled" {
				prefix = "sleep"
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if scenario == "cancelled" {
				bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
				ctx = bounded
			}
			got, err := npmRedirect(ctx, exe, entry, append(os.Environ(), "TF_TEST_NPM_PREFIX="+prefix))
			if scenario == "invalid-prefix" || scenario == "cancelled" {
				if err == nil {
					t.Fatal("invalid prefix resolution was accepted")
				}
				return
			}
			want := entry
			if scenario == "upgraded" {
				want = candidate
			}
			if err != nil || got != want {
				t.Fatalf("entry=%q want=%q err=%v", got, want, err)
			}
		})
	}
}
