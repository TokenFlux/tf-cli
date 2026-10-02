package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("TF_INSTALL_STUB") == "1" {
		dir := os.Getenv("TF_INSTALL_STUB_BIN")
		if len(os.Args) > 1 && os.Args[1] == "--version" {
			if os.Getenv("TF_INSTALL_STUB_BAD_VERSION") == "1" {
				os.Exit(22)
			}
			fmt.Println("fixture 1.2.3")
			os.Exit(0)
		}
		if len(os.Args) > 1 && os.Args[1] == "prefix" {
			if os.Getenv("TF_INSTALL_STUB_BAD_PATH") == "1" {
				fmt.Println("not-an-absolute-path")
			} else {
				fmt.Println(dir)
			}
			os.Exit(0)
		}
		if len(os.Args) > 1 && os.Args[1] == "install" {
			if os.Getenv("TF_INSTALL_STUB_FAIL") == "1" {
				os.Exit(23)
			}
			if os.Getenv("TF_INSTALL_STUB_NO_BIN") != "1" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					panic(err)
				}
				exe, err := os.Executable()
				if err != nil {
					panic(err)
				}
				data, err := os.ReadFile(exe)
				if err != nil {
					panic(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "fixture.exe"), data, 0755); err != nil {
					panic(err)
				}
			}
			os.Exit(0)
		}
		os.Exit(24)
	}
	os.Exit(m.Run())
}

func TestWindowsInstallFindsNewGlobalBin(t *testing.T) {
	for _, scenario := range []string{"success", "install-error", "missing-bin", "bad-path", "bad-version"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			tools := filepath.Join(root, "tools")
			bin := filepath.Join(root, "global bin")
			if err := os.Mkdir(tools, 0700); err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tools, "npm.exe"), data, 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", tools)
			t.Setenv("TF_INSTALL_STUB", "1")
			t.Setenv("TF_INSTALL_STUB_BIN", bin)
			if scenario == "install-error" {
				t.Setenv("TF_INSTALL_STUB_FAIL", "1")
			}
			if scenario == "missing-bin" {
				t.Setenv("TF_INSTALL_STUB_NO_BIN", "1")
			}
			if scenario == "bad-path" {
				t.Setenv("TF_INSTALL_STUB_BAD_PATH", "1")
			}
			if scenario == "bad-version" {
				t.Setenv("TF_INSTALL_STUB_BAD_VERSION", "1")
			}
			h := &Harness{Name: "fixture", Bin: "fixture", Installs: []InstallOption{{Args: []string{"npm", "install", "-g", "fixture"}}}}
			options := h.AvailableInstalls()
			if len(options) != 1 {
				t.Fatalf("options=%v", options)
			}
			err = Install(options[0], os.Stdout, os.Stderr)
			if scenario == "install-error" {
				if err == nil {
					t.Fatal("failed installer was accepted")
				}
				if os.Getenv("PATH") != tools {
					t.Fatal("failed installation changed PATH")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.Detect().Installed {
				t.Fatal("test binary must initially be outside PATH")
			}
			status, err := h.DetectAfterInstall(options[0])
			if scenario == "bad-version" {
				if err == nil || !status.Installed {
					t.Fatalf("unusable client accepted: %+v %v", status, err)
				}
				return
			}
			if scenario != "success" {
				if err == nil || status.Installed {
					t.Fatalf("incomplete install accepted: %v %v", status, err)
				}
				if os.Getenv("PATH") != tools {
					t.Fatal("unverified path was appended")
				}
				return
			}
			if err != nil || !status.Installed || status.Version != "1.2.3" {
				t.Fatalf("status=%+v err=%v", status, err)
			}
			before := os.Getenv("PATH")
			if !strings.HasSuffix(before, string(os.PathListSeparator)+bin) {
				t.Fatalf("PATH=%s", before)
			}
			if _, err := h.DetectAfterInstall(options[0]); err != nil {
				t.Fatal(err)
			}
			if os.Getenv("PATH") != before {
				t.Fatal("repeated detection duplicated PATH")
			}
		})
	}
}
