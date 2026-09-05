package process

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

func TestWindowsNPMShimPreservesArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "space dir")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "fixture")
	if err := os.WriteFile(shim+".cmd", []byte("@exit /b 99\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec \"$TF_TEST_EXECUTABLE\" -test.run=^TestArgHelper$ -- \"$@\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"", "two words", "quote\"here", "&|<>%PATH%!^", "/model/group", "中文", `ends\`, `space end\`, `\"double\"`, `$(touch SHOULD_NOT_EXIST)`, "line\nbreak"}
	env := append(os.Environ(), "TF_TEST_ARGV=1", "TF_TEST_EXECUTABLE="+filepath.ToSlash(exe))
	out, err := CommandContext(context.Background(), shim+".cmd", args, env).CombinedOutput()
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

func TestBatchWithoutPOSIXShimFailsExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.cmd")
	if err := os.WriteFile(path, []byte("@echo unexpected"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := CommandContext(context.Background(), path, nil, nil)
	if cmd.Err == nil {
		t.Fatal("unsupported batch file should not be sent to a command-string shell")
	}
}
