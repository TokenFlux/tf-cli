package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func powerShells(t *testing.T) []string {
	t.Helper()
	var shells []string
	for _, name := range []string{"powershell.exe", "pwsh.exe"} {
		path, err := exec.LookPath(name)
		if err != nil {
			if name == "powershell.exe" || os.Getenv("CI") == "true" {
				t.Fatalf("required PowerShell host missing: %s", name)
			}
			t.Logf("%s is not installed; CI must test both hosts", name)
			continue
		}
		shells = append(shells, path)
	}
	return shells
}

func (f installerFixture) runPowerShell(t *testing.T, shell, script string, purge bool) (string, error) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", script))
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(f.dir, "powershell-test.ps1")
	writeFixture(t, wrapper, []byte(`$ErrorActionPreference = 'Stop'
function Invoke-RestMethod {
 param($Uri, $UserAgent, $TimeoutSec, $Proxy)
 if ($Uri -ne 'https://api.github.com/repos/tokenflux/tf-cli/releases/latest') { throw 'Unexpected metadata URL' }
 $tag = 'v0.0.0'
 if ($env:TF_PS_BAD_TAG) { $tag = $env:TF_PS_BAD_TAG }
 return [pscustomobject]@{tag_name=$tag}
}
function Invoke-WebRequest {
 param($Uri, $UserAgent, $TimeoutSec, $Proxy, $OutFile, [switch]$UseBasicParsing)
 if (-not $Uri.StartsWith('https://github.com/tokenflux/tf-cli/releases/download/v0.0.0/')) { throw 'Unexpected asset URL' }
 [IO.File]::Copy((Join-Path $env:TEST_FIXTURES ([IO.Path]::GetFileName($Uri))), $OutFile, $true)
}
$tls = [Net.ServicePointManager]::SecurityProtocol
try {
 if ($env:TF_PS_IEX -eq '1') {
  $ErrorActionPreference = 'Continue'
  Invoke-Expression ([IO.File]::ReadAllText($env:TF_PS_SCRIPT))
  if ($ErrorActionPreference -ne 'Continue') { throw 'Installer leaked preferences into the session' }
 } elseif ($env:TF_PS_PURGE -eq '1') {
  & $env:TF_PS_SCRIPT -InstallDir $env:TF_INSTALL_DIR -Purge
 } else {
  & $env:TF_PS_SCRIPT -InstallDir $env:TF_INSTALL_DIR
 }
} finally {
 if ([Net.ServicePointManager]::SecurityProtocol -ne $tls) { throw 'Installer did not restore TLS settings' }
}
`), 0600)
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", wrapper)
	// No Git, curl, tar, unzip or Scoop directories in the child PATH.
	cmd.Env = append(f.env, "PATH="+filepath.Join(os.Getenv("WINDIR"), "System32"), "TF_PS_SCRIPT="+path, "TF_PS_PURGE="+map[bool]string{false: "0", true: "1"}[purge])
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestPowerShellInstall(t *testing.T) {
	for _, shell := range powerShells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			for _, scenario := range []string{"new", "replace", "iex", "literal-path", "checksum", "missing-checksum", "duplicate-checksum", "missing-entry", "empty-entry", "download", "locked", "directory", "bad-tag", "unsupported-arch", "non-filesystem"} {
				t.Run(scenario, func(t *testing.T) {
					body, entry := []byte("new binary"), ""
					if scenario == "missing-entry" {
						entry = "../outside.exe"
					}
					if scenario == "empty-entry" {
						body = nil
					}
					f := newInstallerFixture(t, "MINGW64_NT-10.0", "x86_64", body, entry)
					if scenario == "literal-path" {
						dir := filepath.Join(f.dir, "[install] user's directory")
						if err := os.Mkdir(dir, 0700); err != nil {
							t.Fatal(err)
						}
						f.target = filepath.Join(dir, "tf.exe")
						f.env = append(f.env, "TF_INSTALL_DIR="+dir)
					}
					existed := scenario != "new" && scenario != "directory"
					if existed {
						writeFixture(t, f.target, []byte("old binary"), 0755)
					}
					sum := filepath.Join(f.dir, "SHA256SUMS")
					switch scenario {
					case "iex":
						f.env = append(f.env, "TF_PS_IEX=1")
					case "checksum":
						writeFixture(t, sum, []byte(strings.Repeat("0", 64)+"  "+f.asset), 0600)
					case "missing-checksum":
						writeFixture(t, sum, []byte("not a checksum"), 0600)
					case "duplicate-checksum":
						b, _ := os.ReadFile(sum)
						writeFixture(t, sum, append(b, b...), 0600)
					case "download":
						if err := os.Remove(filepath.Join(f.dir, f.asset)); err != nil {
							t.Fatal(err)
						}
					case "locked":
						path, err := windows.UTF16PtrFromString(f.target)
						if err != nil {
							t.Fatal(err)
						}
						handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
						if err != nil {
							t.Fatal(err)
						}
						defer windows.CloseHandle(handle)
					case "directory":
						if err := os.Mkdir(f.target, 0700); err != nil {
							t.Fatal(err)
						}
					case "non-filesystem":
						f.env = append(f.env, `TF_INSTALL_DIR=HKLM:\SOFTWARE`)
					case "bad-tag":
						f.env = append(f.env, "TF_PS_BAD_TAG=v../../bad")
					case "unsupported-arch":
						f.env = append(f.env, "PROCESSOR_ARCHITEW6432=ARM64")
					}
					out, err := f.runPowerShell(t, shell, "install.ps1", false)
					success := scenario == "new" || scenario == "replace" || scenario == "iex" || scenario == "literal-path"
					if success != (err == nil) {
						t.Fatalf("success=%v err=%v output=%s", success, err, out)
					}
					if scenario != "directory" {
						want := "old binary"
						if success {
							want = "new binary"
						}
						got, err := os.ReadFile(f.target)
						if err != nil || string(got) != want {
							t.Fatalf("binary=%q err=%v, want %q", got, err, want)
						}
					}
					leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(f.target), ".tf-install-*"))
					if len(leftovers) > 0 {
						t.Fatalf("staging files leaked: %v", leftovers)
					}
				})
			}
		})
	}
}

func TestPowerShellUninstall(t *testing.T) {
	for _, shell := range powerShells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			f := newInstallerFixture(t, "MINGW64_NT-10.0", "x86_64", []byte("binary"), "")
			writeFixture(t, f.target, []byte("binary"), 0755)
			writeFixture(t, f.target+".old", []byte("backup"), 0755)
			config := filepath.Join(f.dir, "config", "tf")
			if err := os.MkdirAll(config, 0700); err != nil {
				t.Fatal(err)
			}
			credential := filepath.Join(config, "credentials.json")
			writeFixture(t, credential, []byte("fixture credential"), 0600)
			for i := 0; i < 2; i++ {
				if out, err := f.runPowerShell(t, shell, "uninstall.ps1", false); err != nil {
					t.Fatalf("%v: %s", err, out)
				}
			}
			for _, path := range []string{f.target, f.target + ".old"} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("not removed: %s", path)
				}
			}
			if _, err := os.Stat(credential); err != nil {
				t.Fatal("credentials were not preserved")
			}
			outside := filepath.Join(f.dir, "outside")
			if err := os.Mkdir(outside, 0700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(outside, "keep")
			writeFixture(t, sentinel, []byte("keep"), 0600)
			cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", filepath.Join(config, "linked"), outside)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("junction: %v %s", err, out)
			}
			if out, err := f.runPowerShell(t, shell, "uninstall.ps1", true); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if _, err := os.Stat(config); !os.IsNotExist(err) {
				t.Fatal("purge did not remove config")
			}
			if _, err := os.Stat(sentinel); err != nil {
				t.Fatal("purge traversed the directory junction")
			}
		})
	}
}

func TestPowerShellUninstallGuards(t *testing.T) {
	for _, shell := range powerShells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			for _, scenario := range []string{"binary-directory", "backup-directory", "profile", "root-junction"} {
				t.Run(scenario, func(t *testing.T) {
					f := newInstallerFixture(t, "MINGW64_NT-10.0", "x86_64", []byte("binary"), "")
					config := filepath.Join(f.dir, "config", "tf")
					if err := os.MkdirAll(config, 0700); err != nil {
						t.Fatal(err)
					}
					sentinel := filepath.Join(config, "keep")
					writeFixture(t, sentinel, []byte("fixture"), 0600)
					switch scenario {
					case "binary-directory":
						if err := os.Mkdir(f.target, 0700); err != nil {
							t.Fatal(err)
						}
					case "backup-directory":
						writeFixture(t, f.target, []byte("binary"), 0755)
						if err := os.Mkdir(f.target+".old", 0700); err != nil {
							t.Fatal(err)
						}
					case "profile":
						profile := filepath.Join(f.dir, "profile")
						config = filepath.Join(profile, ".tf")
						if err := os.MkdirAll(config, 0700); err != nil {
							t.Fatal(err)
						}
						writeFixture(t, filepath.Join(config, "remove"), []byte("fixture"), 0600)
						f.env = append(f.env, "USERPROFILE="+profile, "XDG_CONFIG_HOME=", "XDG_CACHE_HOME=")
					case "root-junction":
						outside := filepath.Join(f.dir, "outside")
						if err := os.Rename(config, outside); err != nil {
							t.Fatal(err)
						}
						sentinel = filepath.Join(outside, "keep")
						cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", config, outside)
						if out, err := cmd.CombinedOutput(); err != nil {
							t.Fatalf("junction: %v %s", err, out)
						}
					}
					out, err := f.runPowerShell(t, shell, "uninstall.ps1", true)
					wantError := strings.HasSuffix(scenario, "directory")
					if wantError != (err != nil) {
						t.Fatalf("err=%v output=%s", err, out)
					}
					if _, err := os.Stat(sentinel); err != nil {
						t.Fatal("unrelated or protected directory was removed")
					}
					if !wantError {
						if _, err := os.Stat(config); !os.IsNotExist(err) {
							t.Fatal("selected config was not removed")
						}
					}
					if scenario == "backup-directory" {
						if _, err := os.Stat(f.target); err != nil {
							t.Fatal("binary deleted before validating backup")
						}
					}
				})
			}
		})
	}
}

func TestPowerShellInstalledNativeBinary(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, shell := range powerShells(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			f := newInstallerFixture(t, "MINGW64_NT-10.0", "x86_64", body, "")
			if out, err := f.runPowerShell(t, shell, "install.ps1", false); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			cmd := exec.Command(f.target, "-test.run=^TestInstallerBinaryHelper$")
			cmd.Env = append(os.Environ(), "TF_TEST_INSTALLED_BINARY=1")
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != "installed-native-ok" {
				t.Fatalf("%v: %s", err, out)
			}
		})
	}
}
