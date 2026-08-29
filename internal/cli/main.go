package cli

// Main 构造注册表并执行一次调用，返回进程退出码。
func Main(argv []string) int {
	app := NewApp()
	app.Register(newVersionCommand())
	app.Register(newConfigCommand())
	return app.Run(argv)
}
