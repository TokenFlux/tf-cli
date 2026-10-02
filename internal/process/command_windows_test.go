package process

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestArgHelper(t *testing.T) {
	if os.Getenv("TF_TEST_ARGV") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			_ = json.NewEncoder(os.Stdout).Encode(os.Args[i+1:])
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func copyTestExecutable(t *testing.T, path string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0755); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsNPMShimPreservesArgumentsWithoutBash(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "space dir")
	if err := os.Mkdir(prefix, 0700); err != nil {
		t.Fatal(err)
	}
	entry := writePackageBin(t, prefix, filepath.Join(prefix, "node_modules"), "@fixture/client", "fixture", "cli.js")
	copyTestExecutable(t, filepath.Join(prefix, "node.exe"))
	t.Setenv("PATH", prefix)
	args := []string{"", "two words", "quote\"here", "&|<>%PATH%!^", "/model/group", "中文", `ends\`, `space end\`, `\"double\"`, `$(touch SHOULD_NOT_EXIST)`, "line\nbreak"}
	env := append(os.Environ(), "TF_TEST_ARGV=1")
	cmd := CommandContext(context.Background(), filepath.Join(prefix, "fixture.cmd"), append([]string{"--"}, args...), env)
	if cmd.Err != nil {
		t.Fatal(cmd.Err)
	}
	if cmd.Path != filepath.Join(prefix, "node.exe") || cmd.Args[2] != entry {
		t.Fatalf("wrong entry: %v", cmd.Args)
	}
	if !reflect.DeepEqual(cmd.Env, env) {
		t.Fatal("launcher must not rewrite the caller's environment")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("got %#v, want %#v", got, args)
	}
}

func TestWindowsPnpmBinAndNativeEntry(t *testing.T) {
	prefix := t.TempDir()
	entry := writePackageBin(t, prefix, filepath.Join(prefix, "global", "5", "node_modules"), "fixture", "fixture", "fixture.exe")
	copyTestExecutable(t, entry)
	t.Setenv("PATH", prefix)
	cmd := CommandContext(context.Background(), filepath.Join(prefix, "fixture.cmd"), []string{"-test.run=^TestArgHelper$", "--", "a b", "%PATH%"}, append(os.Environ(), "TF_TEST_ARGV=1"))
	if cmd.Err != nil || cmd.Path != entry {
		t.Fatalf("path=%s err=%v", cmd.Path, cmd.Err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a b", "%PATH%"}) {
		t.Fatalf("args=%v", got)
	}
}

func TestWindowsShimMustMatchPackageMetadata(t *testing.T) {
	prefix := t.TempDir()
	root := filepath.Join(prefix, "node_modules")
	first := writePackageBin(t, prefix, root, "first", "fixture", "cli.js")
	firstShim, err := os.ReadFile(filepath.Join(prefix, "fixture.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	second := writePackageBin(t, prefix, root, "second", "fixture", "cli.js")
	got, err := resolvePackageBin(filepath.Join(prefix, "fixture.cmd"))
	if err != nil || got != second {
		t.Fatalf("entry=%s err=%v", got, err)
	}
	f, err := os.OpenFile(filepath.Join(prefix, "fixture.cmd"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(firstShim)
	_ = f.Close()
	if _, err := resolvePackageBin(filepath.Join(prefix, "fixture.cmd")); err == nil {
		t.Fatal("ambiguous metadata accepted")
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	got, err = resolvePackageBin(filepath.Join(prefix, "fixture.cmd"))
	if err != nil || got != second {
		t.Fatalf("missing candidate not ignored: %s %v", got, err)
	}
}

func TestWindowsNodeRuntimeRequired(t *testing.T) {
	prefix := t.TempDir()
	writePackageBin(t, prefix, filepath.Join(prefix, "node_modules"), "fixture", "fixture", "cli.js")
	t.Setenv("PATH", prefix)
	cmd := CommandContext(context.Background(), filepath.Join(prefix, "fixture.cmd"), nil, nil)
	if cmd.Err == nil || !strings.Contains(cmd.Err.Error(), "requires node") {
		t.Fatalf("missing runtime: %v", cmd.Err)
	}
}

func TestCustomBatchFailsExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.cmd")
	if err := os.WriteFile(path, []byte("@echo unexpected"), 0600); err != nil {
		t.Fatal(err)
	}
	if cmd := CommandContext(context.Background(), path, nil, nil); cmd.Err == nil {
		t.Fatal("custom batch files must not be interpreted as command strings")
	}
}
