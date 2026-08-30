package ui

// InteractiveSupported 在 Windows 上是 false。
//
// 交互栈整个建立在 /dev/tty 与 stty 之上，两者都是 Unix 专有：
// 选择器要拿控制终端、隐藏输入要改行规程、子进程退出后要复位终端。
// Windows 得走 CONIN$ 与 SetConsoleMode 重写一遍，还没做。
//
// 之前这里只是让 hasControllingTTY 恒为 false，于是 Windows 用户
// 撞到的是「非交互，请用管道传 Key」—— 那句话在暗示是他的环境有问题，
// 而其实是这个平台压根没有这条路。
const InteractiveSupported = false

func hasControllingTTY() bool { return false }
