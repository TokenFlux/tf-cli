package process

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writePackageBin(t *testing.T, prefix, root, pkg, name, entry string) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(pkg))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"name": pkg, "bin": map[string]string{name: entry}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, entry)
	if err := os.WriteFile(target, []byte("#!/usr/bin/env node -test.run=^TestArgHelper$\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(prefix, target)
	if err != nil {
		t.Fatal(err)
	}
	text := "@echo off\r\n\"%~dp0\\node.exe\" \"%~dp0\\" + rel + "\" %*\r\n"
	if err := os.WriteFile(filepath.Join(prefix, name+".cmd"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestPackageBinLayouts(t *testing.T) {
	for _, layout := range []string{"node_modules", "global/node_modules", "global/5/node_modules"} {
		t.Run(layout, func(t *testing.T) {
			prefix := t.TempDir()
			entry := writePackageBin(t, prefix, filepath.Join(prefix, filepath.FromSlash(layout)), "@fixture/tool", "fixture", "entry.js")
			got, err := resolvePackageBin(filepath.Join(prefix, "fixture.cmd"))
			if err != nil || got != entry {
				t.Fatalf("entry=%q err=%v", got, err)
			}
		})
	}
}

func TestStringBinAndStalePackage(t *testing.T) {
	prefix := t.TempDir()
	root := filepath.Join(prefix, "node_modules")
	writePackageBin(t, prefix, root, "old", "fixture", "old.js")
	entry := writePackageBin(t, prefix, root, "@fixture/fixture", "fixture", "entry.js")
	data, err := json.Marshal(map[string]string{"name": "@fixture/fixture", "bin": "entry.js"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(entry), "package.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := resolvePackageBin(filepath.Join(prefix, "fixture.cmd"))
	if err != nil || got != entry {
		t.Fatalf("entry=%s err=%v", got, err)
	}
}

func TestRejectUnregisteredAndEscapingBin(t *testing.T) {
	prefix := t.TempDir()
	root := filepath.Join(prefix, "node_modules")
	entry := writePackageBin(t, prefix, root, "fixture", "fixture", "entry.js")
	shim := filepath.Join(prefix, "fixture.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\nnot-the-registered-command\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePackageBin(shim); err == nil {
		t.Fatal("unregistered batch accepted")
	}
	data, err := json.Marshal(map[string]any{"name": "fixture", "bin": map[string]string{"fixture": "../outside.js"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(entry), "package.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.js"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := declaredBins(filepath.Dir(entry), "fixture"); len(got) != 0 {
		t.Fatalf("escaping entry accepted: %v", got)
	}
}

func TestBinRuntime(t *testing.T) {
	cases := []struct {
		name, body, runtime string
		flags               []string
		bad                 bool
	}{
		{"plain.js", "console.log('fixture')", "node", nil, false},
		{"node", "#!/usr/bin/env node\n", "node", nil, false},
		{"env-s", "#!/usr/bin/env -S node --no-warnings\n", "node", []string{"--no-warnings"}, false},
		{"bun", "#!/usr/bin/env bun\n", "bun", nil, false},
		{"quoted", "#!/usr/bin/env node --flag='a b'\n", "", nil, true},
		{"shell", "#!/bin/sh\n", "", nil, true},
		{"unknown", "not an executable", "", nil, true},
		{"native.exe", "fixture PE placeholder", "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			runtime, flags, err := binRuntime(file)
			if (err != nil) != tc.bad || runtime != tc.runtime || !slices.Equal(flags, tc.flags) {
				t.Fatalf("runtime=%q flags=%v err=%v", runtime, flags, err)
			}
		})
	}
}
