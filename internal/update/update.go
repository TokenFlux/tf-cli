// Package update 实现自更新：查最新版、下载、校验、原子替换。
//
// 最关键的一步不是下载，是**先认出自己是怎么被装的**。
// npm 装的却去自替换二进制，会在下次 `npm i -g` 时被悄悄覆盖回旧版；
// brew 同理。这类「更新了但又没更新」的状态比不更新更难排查。
package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo 是发布仓库。
const Repo = "tokenflux/tkr"

// Source 是 tkr 自身的安装来源。
type Source string

const (
	// SourceBinary 是独立二进制，可以自替换。
	SourceBinary Source = "binary"
	// SourceNPM 由 npm / pnpm / bun 全局安装。
	SourceNPM Source = "npm"
	// SourceHomebrew 由 brew 安装。
	SourceHomebrew Source = "homebrew"
	// SourceGoInstall 由 go install 安装。
	SourceGoInstall Source = "go"
	// SourceDevel 是 go run 或本地 make build 的产物。
	SourceDevel Source = "devel"
)

// UpgradeCommand 返回该来源应当使用的升级命令；自替换时返回空串。
func (s Source) UpgradeCommand() string {
	switch s {
	case SourceNPM:
		return "npm install -g tf@latest"
	case SourceHomebrew:
		return "brew upgrade tf"
	case SourceGoInstall:
		return "go install github.com/tokenflux/tkr/cmd/tf@latest"
	case SourceDevel:
		return "git pull && make build"
	}
	return ""
}

// DetectSource 依据可执行文件的位置判断安装来源。
//
// 路径是唯一可靠的线索：包管理器都会把二进制放进自己的目录结构里。
func DetectSource(exe string) Source {
	resolved, err := filepath.EvalSymlinks(exe)
	if err == nil {
		exe = resolved
	}
	p := filepath.ToSlash(exe)

	switch {
	case strings.Contains(p, "/node_modules/"):
		return SourceNPM
	case strings.Contains(p, "/Cellar/") || strings.Contains(p, "/homebrew/"):
		return SourceHomebrew
	case strings.Contains(p, "/pkg/mod/") || strings.HasSuffix(filepath.Dir(p), "/go/bin"):
		return SourceGoInstall
	// go run 会把二进制放进临时目录。
	case strings.Contains(p, "/go-build"):
		return SourceDevel
	}
	return SourceBinary
}

// ErrNoRelease 表示仓库还没有任何发布。
//
// 与网络故障分开：报成网络问题会让用户白查半天网络。
var ErrNoRelease = errors.New("no releases yet")

// Release 是一次发布。
type Release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Version 去掉 tag 前缀的 v。
func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// Latest 查询最新发布。
func Latest(ctx context.Context, client *http.Client) (*Release, error) {
	url := "https://api.github.com/repos/" + Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoRelease
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GitHub API %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// AssetName 推导当前平台的归档名。
//
// 必须与 .github/workflows/release.yml 的打包规则严格一致 ——
// 两处各写一遍就迟早会漂。
func AssetName(version string) string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("tf_%s_%s_%s%s", version, runtime.GOOS, runtime.GOARCH, ext)
}

// Newer 报告 remote 是否比 local 新。
//
// 只做语义化版本的数值比较。local 不是正式版本号（dev 等）时一律认为需要更新。
func Newer(local, remote string) bool {
	if local == "" || local == "dev" {
		return true
	}
	lp, rp := splitVersion(local), splitVersion(remote)
	for i := 0; i < 3; i++ {
		if lp[i] != rp[i] {
			return rp[i] > lp[i]
		}
	}
	return false
}

func splitVersion(v string) [3]int {
	var out [3]int
	// 丢掉 -rc.1 之类的后缀再比。
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for i, part := range strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3) {
		if i > 2 {
			break
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return out
		}
		out[i] = n
	}
	return out
}

// Apply 下载归档、按 SHA256SUMS 校验、原子替换当前可执行文件。
//
// 校验和是必须的：更新会把一个可执行文件放到用户的 PATH 上，
// 传输中途损坏或被中间人替换的后果不可接受。
func Apply(ctx context.Context, client *http.Client, rel *Release, exe string) error {
	want := AssetName(rel.Version())

	var assetURL, sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case want:
			assetURL = a.URL
		case "SHA256SUMS":
			sumsURL = a.URL
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no asset %s in %s", want, rel.Tag)
	}
	if sumsURL == "" {
		return errors.New("release has no SHA256SUMS")
	}

	archive, err := download(ctx, client, assetURL)
	if err != nil {
		return err
	}
	sums, err := download(ctx, client, sumsURL)
	if err != nil {
		return err
	}
	if err := verify(archive, sums, want); err != nil {
		return err
	}

	bin, err := extract(archive)
	if err != nil {
		return err
	}
	return replace(exe, bin)
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("download failed with %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

// verify 比对归档的 SHA256 与 SHA256SUMS 中的记录。
func verify(archive, sums []byte, name string) error {
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])

	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if fields[0] != got {
			return fmt.Errorf("checksum mismatch for %s", name)
		}
		return nil
	}
	return fmt.Errorf("no checksum recorded for %s", name)
}

// extract 从归档里取出 tkr 可执行文件。
func extract(archive []byte) ([]byte, error) {
	if runtime.GOOS == "windows" {
		zr, err := zip.NewReader(newByteReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) != "tf.exe" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 128<<20))
		}
		return nil, errors.New("archive contains no tf.exe")
	}

	gz, err := gzip.NewReader(newByteReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == "tf" {
			return io.ReadAll(io.LimitReader(tr, 128<<20))
		}
	}
	return nil, errors.New("archive contains no tf")
}

// replace 原子替换可执行文件。
//
// 临时文件必须落在目标同目录：跨文件系统 rename 会失败，
// 而 /tmp 与 /usr/local/bin 常常就不是同一个文件系统。
func replace(exe string, data []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".tf-update-*")
	if err != nil {
		return fmt.Errorf("%s is not writable: %w", dir, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}

	// Windows 不允许改名覆盖正在运行的文件，先把旧的挪开。
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), exe)
}

// newByteReader 让 []byte 满足 io.ReaderAt。
func newByteReader(b []byte) *byteReader { return &byteReader{b} }

type byteReader struct{ b []byte }

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func (r *byteReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r.b)) {
		return 0, io.EOF
	}
	n := copy(p, r.b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// DefaultClient 是带超时的客户端。下载可能较慢，但不能无限等。
func DefaultClient() *http.Client { return &http.Client{Timeout: 5 * time.Minute} }
