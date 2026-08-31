// Package buildinfo 保存构建期注入的版本信息。
//
// 通过 -ldflags 注入：
//
//	go build -ldflags "-X github.com/tokenflux/tkr/internal/buildinfo.Version=0.1.0"
package buildinfo

var (
	// Version 是语义化版本号。
	Version = "dev"
	// Commit 是构建所用的 git 短哈希。
	Commit = "unknown"
)

// UserAgent 返回 tf 自身请求使用的 UA。
//
// 注意：这个 UA 只用于 tf 自己发起的请求（目录、状态查询）。
// 绝不能出现在注入给 harness 的环境里 —— 覆盖 harness 的 UA 会破坏
// claude_code_only 分组的 UA + TLS 指纹识别。见 docs/design/product-decisions.md 第 0 节。
func UserAgent() string {
	return "tf/" + Version
}
