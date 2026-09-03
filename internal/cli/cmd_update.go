package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/tokenflux/tf-cli/internal/buildinfo"
	"github.com/tokenflux/tf-cli/internal/ui"
	"github.com/tokenflux/tf-cli/internal/update"
)

func newUpdateCommand() *Command {
	return &Command{
		Name:  "update",
		Usage: "tf update [--check]",
		Summary: func(u *ui.UI) string {
			return u.T("更新 tf 自身", "Update tf itself")
		},
		Flags: []Flag{
			{Name: "check", Kind: KindBool,
				Desc: "只检查有无新版，不安装||Only check for a newer version"},
		},
		Run: runUpdate,
	}
}

func runUpdate(c *Context) error {
	exe, err := os.Executable()
	if err != nil {
		return ui.Errf(ui.CodeInternal,
			c.UI.T("无法定位当前可执行文件", "cannot locate the running executable")).WithCause(err)
	}
	source := update.DetectSource(exe)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := update.DefaultClient()
	rel, err := update.Latest(ctx, client)
	if errors.Is(err, update.ErrNoRelease) {
		return ui.Errf(ui.CodeUsage,
			c.UI.T("这个仓库还没有发布任何版本", "this repository has no releases yet")).
			WithHint(source.UpgradeCommand())
	}
	if err != nil {
		return ui.Errf(ui.CodeNetwork,
			c.UI.T("无法查询最新版本", "cannot check for the latest version")).WithCause(err)
	}

	current := buildinfo.Version
	newer := update.Newer(current, rel.Version())

	if !newer {
		c.UI.Emit("update", map[string]any{
			"current": current, "latest": rel.Version(), "update_available": false,
		}, func() {
			c.UI.Printf("%s\n", fmt.Sprintf(
				c.UI.T("已是最新：%s", "already up to date: %s"), current))
		})
		return nil
	}

	if c.Flags.Bool("check") {
		c.UI.Emit("update", map[string]any{
			"current": current, "latest": rel.Version(), "update_available": true,
			"source": string(source),
		}, func() {
			c.UI.Printf("%s\n", fmt.Sprintf(
				c.UI.T("有新版本：%s → %s", "update available: %s → %s"), current, rel.Version()))
		})
		return nil
	}

	// 包管理器装的不能自替换：下次 `npm i -g` / `brew upgrade` 会把旧版
	// 悄悄换回来，留下「更新了但又没更新」的状态，比不更新更难排查。
	if cmd := source.UpgradeCommand(); cmd != "" {
		return ui.Errf(ui.CodeUsage, fmt.Sprintf(
			c.UI.T("tf 由 %s 安装，请用它升级（%s → %s）",
				"tf was installed via %s; upgrade with it (%s → %s)"),
			source, current, rel.Version())).WithHint(cmd)
	}

	c.UI.Logf("%s", c.UI.Dim(fmt.Sprintf(
		c.UI.T("正在更新 %s → %s…", "updating %s → %s…"), current, rel.Version())))

	if err := update.Apply(ctx, client, rel, exe); err != nil {
		// 目录不可写是最常见的失败，单独给出可执行的下一步。
		//
		// 不建议 sudo：装 harness 时代码硬拒提权命令，自更新却把 sudo
		// 推给用户，同一个产品不能有两套安全观。而且真正的问题是这份
		// 二进制装在了需要 root 才能写的地方 —— 重装到用户目录才是解法。
		hint := "curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/install.sh | sh"
		if os.IsPermission(err) {
			return ui.Errf(ui.CodeInstallFailed,
				c.UI.T("没有写入权限", "no permission to replace the binary")).
				WithCause(err).WithHint(hint)
		}
		return ui.Errf(ui.CodeInstallFailed,
			c.UI.T("更新失败", "update failed")).WithCause(err)
	}

	c.UI.Emit("update", map[string]any{
		"current": current, "latest": rel.Version(), "updated": true,
	}, func() {
		c.UI.Printf("✓ %s\n", fmt.Sprintf(
			c.UI.T("已更新到 %s", "updated to %s"), rel.Version()))
	})
	return nil
}
