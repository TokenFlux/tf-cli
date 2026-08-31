package cli

import "github.com/tokenflux/tkr/internal/harness"

// allCommands 是唯一的命令名单。
//
// 补全也从这里取。之前补全里另有一份手写的名单，加了 tf status 之后
// 那份不知道 —— 两份长得一样，看代码时不会觉得有问题。
func allCommands() []*Command {
	cmds := []*Command{
		newVersionCommand(),
		newConfigCommand(),
		newStatusCommand(),
		newLoginCommand(),
		newLogoutCommand(),
		newKeysCommand(),
		newUpdateCommand(),
		newHarnessCommand(),
		newModelCommand(),
		newCompletionsCommand(),
		newCompleteCommand(),
	}

	// 每个 harness 都是一个透传型子命令：tf claude / tf codex / ...
	for _, h := range harness.All {
		cmds = append(cmds, newLaunchCommand(h))
	}
	return cmds
}

// Main 构造注册表并执行一次调用，返回进程退出码。
func Main(argv []string) int {
	app := NewApp()
	for _, c := range allCommands() {
		app.Register(c)
	}
	return app.Run(argv)
}
