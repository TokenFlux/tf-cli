package scripts

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type installerFixture struct {
	dir, bin, target, asset string
	env                     []string
}

func writeFixture(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func newInstallerFixture(t *testing.T, platform, arch string, body []byte, entry string) installerFixture {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "tools")
	target := filepath.Join(dir, "install space dir")
	for _, path := range []string{bin, target} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	osName := strings.ToLower(platform)
	binary, extension := "tf", "tar.gz"
	if strings.HasPrefix(osName, "mingw") || strings.HasPrefix(osName, "msys") || strings.HasPrefix(osName, "cygwin") {
		osName = "windows"
		binary = "tf.exe"
		extension = "zip"
	}
	assetArch := arch
	if arch == "x86_64" {
		assetArch = "amd64"
	}
	if arch == "aarch64" {
		assetArch = "arm64"
	}
	asset := fmt.Sprintf("tf_0.0.0_%s_%s.%s", osName, assetArch, extension)
	if entry == "" {
		entry = binary
	}
	var archive bytes.Buffer
	if extension == "zip" {
		zw := zip.NewWriter(&archive)
		w, err := zw.Create(entry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: entry, Size: int64(len(body)), Mode: 0755}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, filepath.Join(dir, asset), archive.Bytes(), 0600)
	sum := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive.Bytes()), asset)
	writeFixture(t, filepath.Join(dir, "SHA256SUMS"), []byte(sum), 0600)
	writeFixture(t, filepath.Join(bin, "uname"), []byte("#!/bin/sh\nif [ \"$1\" = -s ]; then printf '%s\\n' \"$TEST_OS\"; else printf '%s\\n' \"$TEST_ARCH\"; fi\n"), 0755)
	if runtime.GOOS != "windows" {
		writeFixture(t, filepath.Join(bin, "cygpath"), []byte("#!/bin/sh\nprintf '%s\\n' \"$2\"\n"), 0755)
	}
	writeFixture(t, filepath.Join(bin, "curl"), []byte(`#!/bin/sh
set -eu
url= out=
while [ "$#" -gt 0 ]; do
 case "$1" in
  -o) out=$2; shift 2 ;;
  -w) shift 2 ;;
  -fsSL) shift ;;
  https://*) url=$1; shift ;;
  *) exit 91 ;;
 esac
done
case "$url" in
 https://github.com/tokenflux/tf-cli/releases/latest) printf 'https://github.com/tokenflux/tf-cli/releases/tag/v0.0.0' ;;
 https://github.com/tokenflux/tf-cli/releases/download/v0.0.0/*) cp "$TEST_FIXTURES/${url##*/}" "$out" ;;
 *) exit 92 ;;
esac
`), 0755)
	env := append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "TEST_FIXTURES="+dir, "TEST_OS="+platform, "TEST_ARCH="+arch, "TF_INSTALL_DIR="+target, "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"))
	return installerFixture{dir: dir, bin: bin, target: filepath.Join(target, binary), asset: asset, env: env}
}

func (f installerFixture) run(t *testing.T, script string, args ...string) (string, error) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", script))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{path}, args...)...)
	cmd.Env = f.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestInstallArchives(t *testing.T) {
	for _, platform := range []string{"Linux", "Darwin", "MINGW64_NT-10.0", "MSYS_NT-10.0", "CYGWIN_NT-10.0"} {
		t.Run(platform, func(t *testing.T) {
			f := newInstallerFixture(t, platform, "x86_64", []byte("new binary"), "")
			writeFixture(t, f.target, []byte("old binary"), 0755)
			if out, err := f.run(t, "install.sh"); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			got, err := os.ReadFile(f.target)
			if err != nil || string(got) != "new binary" {
				t.Fatalf("got=%q err=%v", got, err)
			}
			if runtime.GOOS != "windows" {
				info, err := os.Stat(f.target)
				if err != nil || info.Mode().Perm() != 0755 {
					t.Fatalf("installed permissions: %v %v", info, err)
				}
			}
			leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(f.target), ".tf-install.*"))
			if len(leftovers) > 0 {
				t.Fatalf("staging files leaked: %v", leftovers)
			}
		})
	}
}

func TestInstallFailurePreservesOldBinary(t *testing.T) {
	for _, failure := range []string{"checksum", "missing-checksum", "duplicate-checksum", "missing-entry", "empty-entry", "download", "replacement", "unsupported-arch"} {
		t.Run(failure, func(t *testing.T) {
			body, entry, arch := []byte("new binary"), "", "x86_64"
			if failure == "missing-entry" {
				entry = "unexpected.exe"
			}
			if failure == "empty-entry" {
				body = nil
			}
			if failure == "unsupported-arch" {
				arch = "aarch64"
			}
			f := newInstallerFixture(t, "MINGW64_NT-10.0", arch, body, entry)
			writeFixture(t, f.target, []byte("old binary"), 0755)
			sumPath := filepath.Join(f.dir, "SHA256SUMS")
			switch failure {
			case "checksum":
				writeFixture(t, sumPath, []byte(strings.Repeat("0", 64)+"  "+f.asset+"\n"), 0600)
			case "missing-checksum":
				writeFixture(t, sumPath, []byte("abcd  another.zip\n"), 0600)
			case "duplicate-checksum":
				b, _ := os.ReadFile(sumPath)
				writeFixture(t, sumPath, append(b, b...), 0600)
			case "download":
				if err := os.Remove(filepath.Join(f.dir, f.asset)); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				writeFixture(t, filepath.Join(f.bin, "mv"), []byte("#!/bin/sh\nexit 1\n"), 0755)
			}
			if out, err := f.run(t, "install.sh"); err == nil {
				t.Fatalf("failure accepted: %s", out)
			}
			got, err := os.ReadFile(f.target)
			if err != nil || string(got) != "old binary" {
				t.Fatalf("old binary lost: %q %v", got, err)
			}
			leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(f.target), ".tf-install.*"))
			if len(leftovers) > 0 {
				t.Fatalf("staging files leaked: %v", leftovers)
			}
		})
	}
}

func TestWindowsUninstall(t *testing.T) {
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
		if out, err := f.run(t, "uninstall.sh"); err != nil {
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
	if out, err := f.run(t, "uninstall.sh", "--purge"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatal("purge did not remove fixture config")
	}
}

func TestWindowsPurgeUsesNativeProfile(t *testing.T) {
	f := newInstallerFixture(t, "MINGW64_NT-10.0", "x86_64", []byte("binary"), "")
	profile := filepath.Join(f.dir, "native-profile")
	nativeConfig := filepath.Join(profile, ".tf")
	shellConfig := filepath.Join(f.dir, ".tf")
	for _, path := range []string{nativeConfig, shellConfig} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(path, "keep"), []byte("fixture"), 0600)
	}
	f.env = append(f.env, "USERPROFILE="+profile, "XDG_CONFIG_HOME=", "XDG_CACHE_HOME=")
	if out, err := f.run(t, "uninstall.sh", "--purge"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if _, err := os.Stat(nativeConfig); !os.IsNotExist(err) {
		t.Fatal("native config was not removed")
	}
	if _, err := os.Stat(filepath.Join(shellConfig, "keep")); err != nil {
		t.Fatal("purge removed the shell HOME config instead")
	}
}

func TestWindowsScriptsRefuseDirectories(t *testing.T) {
	for _, backup := range []bool{false, true} {
		t.Run(fmt.Sprint("backup=", backup), func(t *testing.T) {
			f := newInstallerFixture(t, "MINGW64_NT-10.0", "x86_64", []byte("binary"), "")
			path := f.target
			if backup {
				writeFixture(t, f.target, []byte("old binary"), 0755)
				path += ".old"
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(path, "keep")
			writeFixture(t, sentinel, []byte("keep"), 0600)
			if !backup {
				if out, err := f.run(t, "install.sh"); err == nil {
					t.Fatalf("installed over directory: %s", out)
				}
			}
			if out, err := f.run(t, "uninstall.sh"); err == nil {
				t.Fatalf("removed directory: %s", out)
			}
			if _, err := os.Stat(sentinel); err != nil {
				t.Fatal("directory contents were removed")
			}
			if backup {
				if _, err := os.Stat(f.target); err != nil {
					t.Fatal("partially uninstalled before validation")
				}
			}
		})
	}
}

func TestInstallerBinaryHelper(t *testing.T) {
	if os.Getenv("TF_TEST_INSTALLED_BINARY") == "1" {
		fmt.Print("installed-native-ok")
		os.Exit(0)
	}
}

func TestInstalledNativeBinaryRuns(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	platform := runtime.GOOS
	if platform == "windows" {
		platform = "MINGW64_NT-10.0"
	}
	f := newInstallerFixture(t, platform, runtime.GOARCH, body, "")
	if out, err := f.run(t, "install.sh"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	cmd := exec.Command(f.target, "-test.run=^TestInstallerBinaryHelper$")
	cmd.Env = append(os.Environ(), "TF_TEST_INSTALLED_BINARY=1")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "installed-native-ok" {
		t.Fatalf("%v: %s", err, out)
	}
}
