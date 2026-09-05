package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStaleLoginCannotResurrectLoggedOutKey(t *testing.T) {
	p := testPaths(t)
	cfg, creds, _, err := LoadState(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg.KeyMetaOf("old")
	creds.Set("old", &Credential{Key: "sk-old"})
	if err := SaveState(cfg, creds); err != nil {
		t.Fatal(err)
	}
	loginCfg, loginCreds, _, _ := LoadState(p)
	logoutCfg, logoutCreds, _, _ := LoadState(p)
	delete(logoutCfg.Keys, "old")
	logoutCreds.Remove("old")
	if err := SaveState(logoutCfg, logoutCreds); err != nil {
		t.Fatal(err)
	}
	loginCfg.KeyMetaOf("new")
	loginCreds.Set("new", &Credential{Key: "sk-new"})
	if err := SaveState(loginCfg, loginCreds); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	actualCfg, actualCreds, _, err := LoadState(p)
	if err != nil || len(actualCfg.Keys) != 0 || len(actualCreds.Items) != 0 {
		t.Fatalf("stale snapshot was persisted: %v %v %v", actualCfg, actualCreds, err)
	}
}

func TestConcurrentSavesRejectStaleSnapshots(t *testing.T) {
	p := testPaths(t)
	a, _ := Load(p)
	b, _ := Load(p)
	a.Harness("codex").Key = "a"
	b.Harness("codex").Key = "b"
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, cfg := range []*Config{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); results <- cfg.Save() }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestRecoverInterruptedStateTransaction(t *testing.T) {
	p := testPaths(t)
	cfg, creds, _, _ := LoadState(p)
	cfg.KeyMetaOf("new")
	creds.Set("new", &Credential{Key: "sk-secret"})
	configData, _ := marshalSnapshot(cfg)
	credsData, _ := marshalSnapshot(creds)
	journal, _ := marshalSnapshot(transaction{Config: configData, Credentials: credsData})
	path := filepath.Join(p.ConfigDir, journalName)
	if err := writeAtomic(path, journal, credsFilePerm); err != nil {
		t.Fatal(err)
	}
	assertPermissions(t, path, 0600)
	// Simulate termination after only the credential replacement.
	if err := writeAtomic(p.CredentialsFile(), credsData, credsFilePerm); err != nil {
		t.Fatal(err)
	}
	gotCfg, gotCreds, _, err := LoadState(p)
	if err != nil || gotCfg.Keys["new"] == nil || gotCreds.Items["new"].Key != "sk-secret" {
		t.Fatalf("recovery failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("journal not cleaned up")
	}
	if err := SaveState(gotCfg, gotCreds); err != nil {
		t.Fatalf("recovered snapshots cannot save: %v", err)
	}
}

func TestCredentialOnlySaveRejectsStaleSnapshot(t *testing.T) {
	p := testPaths(t)
	a, _, _ := LoadCredentials(p)
	b, _, _ := LoadCredentials(p)
	a.Set("a", &Credential{Key: "sk-a"})
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	b.Set("b", &Credential{Key: "sk-b"})
	if err := b.Save(); !errors.Is(err, ErrConflict) {
		t.Fatalf("%v", err)
	}
}
