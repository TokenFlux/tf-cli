package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 安装来源决定能不能自替换 —— 认错了会留下「更新了但又没更新」的状态。
func TestDetectSource(t *testing.T) {
	cases := []struct {
		path string
		want Source
	}{
		{"/opt/homebrew/lib/node_modules/tf/bin/tf", SourceNPM},
		{"/Users/x/.bun/install/global/node_modules/tf/bin/tf", SourceNPM},
		{"/opt/homebrew/Cellar/tkr/0.1.0/bin/tkr", SourceHomebrew},
		{"/home/linuxbrew/.linuxbrew/Cellar/tkr/0.1.0/bin/tkr", SourceHomebrew},
		{"/Users/x/go/bin/tkr", SourceGoInstall},
		{"/var/folders/xx/go-build123/b001/exe/tkr", SourceDevel},
		{"/usr/local/bin/tkr", SourceBinary},
		{"/Users/x/.local/bin/tkr", SourceBinary},
	}
	for _, c := range cases {
		if got := DetectSource(c.path); got != c.want {
			t.Errorf("DetectSource(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// 包管理器装的必须给出对应的升级命令，独立二进制才自替换。
func TestUpgradeCommand(t *testing.T) {
	if SourceBinary.UpgradeCommand() != "" {
		t.Error("a standalone binary should self-replace, not defer to a manager")
	}
	for _, s := range []Source{SourceNPM, SourceHomebrew, SourceGoInstall, SourceDevel} {
		if s.UpgradeCommand() == "" {
			t.Errorf("%s must provide an upgrade command", s)
		}
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		local, remote string
		want          bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.2.0", "0.1.0", false},
		{"0.1.0", "0.1.0", false},
		{"0.9.0", "0.10.0", true}, // 数值比较，不是字典序
		{"1.0.0", "0.99.9", false},
		{"0.1.0", "0.1.1", true},
		{"dev", "0.1.0", true},         // 开发版一律认为需要更新
		{"", "0.1.0", true},            // 版本缺失同理
		{"0.1.0-rc.1", "0.1.0", false}, // 预发布后缀被忽略
	}
	for _, c := range cases {
		if got := Newer(c.local, c.remote); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.local, c.remote, got, c.want)
		}
	}
}

// 归档名必须与 release.yml 的打包规则一致，否则自更新找不到文件。
func TestAssetName(t *testing.T) {
	got := AssetName("0.1.0")
	want := fmt.Sprintf("tf_0.1.0_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		want += ".zip"
	} else {
		want += ".tar.gz"
	}
	if got != want {
		t.Errorf("AssetName = %q, want %q", got, want)
	}
}

// 校验和不符必须拒绝 —— 更新会把可执行文件放到用户的 PATH 上。
func TestVerify(t *testing.T) {
	archive := []byte("pretend this is a tarball")
	sum := sha256.Sum256(archive)
	name := "tf_0.1.0_linux_amd64.tar.gz"
	good := fmt.Sprintf("%s  %s\nffff  other.tar.gz\n", hex.EncodeToString(sum[:]), name)

	if err := verify(archive, []byte(good), name); err != nil {
		t.Errorf("matching checksum should pass: %v", err)
	}
	if err := verify([]byte("tampered"), []byte(good), name); err == nil {
		t.Error("a mismatching archive must be rejected")
	}
	if err := verify(archive, []byte("ffff  other.tar.gz\n"), name); err == nil {
		t.Error("a missing checksum entry must be rejected, not skipped")
	}
}

// 能从 tar.gz 里取出二进制。
func TestExtractTarGz(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses zip")
	}
	want := []byte("#!/bin/sh\necho tkr\n")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "tf", Mode: 0o755, Size: int64(len(want))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(want); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()

	got, err := extract(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extracted %q, want %q", got, want)
	}
}

// 替换必须是原子的，且新文件要可执行。
func TestReplace(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tf")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := replace(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want new", got)
	}
	st, _ := os.Stat(exe)
	if st.Mode().Perm()&0o111 == 0 {
		t.Errorf("replacement is not executable: %o", st.Mode().Perm())
	}

	// 临时文件必须落在同目录，否则跨文件系统改名会失败。
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

// 目录不可写时要报错，而不是悄悄成功。
func TestReplaceUnwritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "tf")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)

	if err := replace(exe, []byte("new")); err == nil {
		t.Error("expected an error when the directory is not writable")
	}
}
