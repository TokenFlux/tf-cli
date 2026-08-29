package cli

import "github.com/tokenflux/tkr/internal/harness"

// Main 构造注册表并执行一次调用，返回进程退出码。
func Main(argv []string) int {
	app := NewApp()
	app.Register(newVersionCommand())
	app.Register(newConfigCommand())
	app.Register(newLoginCommand())
	app.Register(newLogoutCommand())
	app.Register(newHarnessCommand())
	app.Register(newModelCommand())
	app.Register(newCompletionsCommand())
	app.Register(newCompleteCommand())

	// 每个 harness 都是一个透传型子命令：tkr claude / tkr codex / ...
	for _, h := range harness.All {
		app.Register(newLaunchCommand(h))
	}
	return app.Run(argv)
}
