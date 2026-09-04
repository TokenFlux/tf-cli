package ui

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// Item 是选择器里的一行。
type Item struct {
	Label  string // 主文本，也是过滤依据
	Detail string // 右侧灰色补充信息
	Note   string // 行尾标记，如 ← 当前
	// Disabled 的项会展示但不能选中，光标也会跳过它。
	//
	// 用于「功能存在但当前不可用」：直接不显示会让用户以为没有这个能力，
	// 显示成可选又会让人白选一次。
	Disabled bool
}

// Select 展示一个可用方向键操作的列表，返回选中项下标。
//
// 无法进入 raw 模式时自动降级为编号选择器 —— 交互降级永远比报错好。
func (u *UI) Select(title string, items []Item) (int, error) {
	return u.SelectWith(title, items, SelectOpt{})
}

// SelectOpt 调整选择器的行为。
type SelectOpt struct {
	// CancelHint 是提示行里 esc 那一格的文案。取消在不同界面上的后果不同
	// （退出启动、退回上一层、结束编辑），提示行必须说的是这一屏的后果。
	// 留空则为「取消」。
	CancelHint string
}

// SelectWith 同 Select，可指定 esc 的说明文案。
func (u *UI) SelectWith(title string, items []Item, opt SelectOpt) (int, error) {
	if len(items) == 0 {
		return 0, Errf(CodeInternal, "no items to select")
	}

	tty, err := openRawTTY()
	if err != nil {
		// 降级到编号选择器时，不可用项也必须标出来，
		// 否则用户会选一个号码然后被拒绝，却不知道为什么。
		labels := make([]string, len(items))
		for i, it := range items {
			label := strings.TrimSpace(it.Label + "  " + it.Detail)
			if it.Disabled {
				label += "  " + u.T("（暂不可用）", "(unavailable)")
			}
			labels[i] = label
		}
		for {
			pick, err := u.Choose(title, labels)
			if err != nil {
				return 0, err
			}
			if !items[pick].Disabled {
				return pick, nil
			}
			u.Warnf("%s", u.T("该选项暂不可用", "that option is not available yet"))
		}
	}

	// 终端已进入 raw 模式，此时被信号打断会让终端处于不可用状态，
	// 所以必须在信号处理里也恢复一次。
	restore := make(chan os.Signal, 1)
	signal.Notify(restore, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-restore:
			tty.Restore()
			os.Exit(130)
		case <-done:
		}
	}()
	defer func() {
		close(done)
		signal.Stop(restore)
		tty.Restore()
	}()

	s := &selector{ui: u, tty: tty, title: title, all: items, cursor: firstEnabled(items), opt: opt}
	return s.run()
}

type selector struct {
	ui     *UI
	tty    *rawTTY
	title  string
	all    []Item
	query  string
	cursor int
	offset int
	drawn  int
	opt    SelectOpt
}

func (s *selector) run() (int, error) {
	for {
		view := s.filtered()
		// cursor 为 -1 表示「过滤条件变了，重新定位」。
		if s.cursor < 0 || s.cursor >= len(view) {
			s.cursor = firstEnabledView(view)
		}
		s.draw(view)

		k, r := s.tty.readKey()
		switch k {
		case keyUp:
			if n := s.seek(view, s.cursor, -1); n >= 0 {
				s.cursor = n
			}
		case keyDown:
			if n := s.seek(view, s.cursor, +1); n >= 0 {
				s.cursor = n
			}
		case keyEnter:
			// 光标不会停在不可用项上，这里只是兼顾全都不可用的情形。
			if len(view) == 0 || view[s.cursor].Disabled {
				continue
			}
			s.clear()
			return view[s.cursor].index, nil
		case keyEscape:
			if s.escape() {
				continue
			}
			s.clear()
			return 0, Errf(CodeCancelled, s.ui.T("已取消", "cancelled"))
		case keyCancel:
			s.clear()
			return 0, Errf(CodeCancelled, s.ui.T("已取消", "cancelled"))
		case keyClear:
			s.query = ""
			s.cursor = -1
		case keyBackspace:
			if s.query != "" {
				s.query = s.query[:len(s.query)-1]
				s.cursor = -1 // 过滤后重新定位到首个可选项
			}
		case keyRune:
			s.query += string(r)
			s.cursor = -1
		}
	}
}

// escape 处理裸 ESC：先退掉过滤，返回 true 表示已消化。
//
// 没这一步的话，打错一个字的唯一出路是退出整个选择器。
func (s *selector) escape() bool {
	if s.query == "" {
		return false
	}
	s.query = ""
	s.cursor = -1
	return true
}

type viewItem struct {
	Item
	index int
}

// filtered 按输入的子串过滤。列表长时这是必需的，短时也无害。
//
// Label 与 Detail 都参与匹配：屏幕上写着 ccmax/、work，用户就会照着敲，
// 而分组前缀和 Key 名恰好只出现在 Detail 里。只匹配 Label 的结果是
// 照着屏幕输入却得到「无匹配项」。
//
// Label 命中的排在前面：那是这一行的主体，比在备注里撞上更像用户要找的。
func (s *selector) filtered() []viewItem {
	q := strings.ToLower(s.query)
	if q == "" {
		out := make([]viewItem, 0, len(s.all))
		for i, it := range s.all {
			out = append(out, viewItem{Item: it, index: i})
		}
		return out
	}

	var primary, secondary []viewItem
	for i, it := range s.all {
		switch {
		case strings.Contains(strings.ToLower(it.Label), q):
			primary = append(primary, viewItem{Item: it, index: i})
		case strings.Contains(strings.ToLower(it.Detail), q),
			strings.Contains(strings.ToLower(it.Note), q):
			secondary = append(secondary, viewItem{Item: it, index: i})
		}
	}
	return append(primary, secondary...)
}

// seek 沿 step 方向找下一个可选项；没有则返回 -1。
//
// 跨过不可用项而不是停在上面：光标停在一个按回车没反应的条目上，
// 比不能选本身更让人困惑。
func (s *selector) seek(view []viewItem, from, step int) int {
	for i := from + step; i >= 0 && i < len(view); i += step {
		if !view[i].Disabled {
			return i
		}
	}
	return -1
}

// firstEnabled 返回首个可选项的下标，全都不可选时返回 0。
func firstEnabled(items []Item) int {
	for i, it := range items {
		if !it.Disabled {
			return i
		}
	}
	return 0
}

// firstEnabledView 同上，但作用于过滤后的视图。
func firstEnabledView(view []viewItem) int {
	for i, it := range view {
		if !it.Disabled {
			return i
		}
	}
	return 0
}

// window 计算可见区间，保证光标始终在视野内。
func (s *selector) window(n int) (start, end int) {
	rows, _ := s.tty.Size()
	maxRows := rows - 4 // 标题、提示行、余量
	if maxRows < 3 {
		maxRows = 3
	}
	if n <= maxRows {
		s.offset = 0
		return 0, n
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+maxRows {
		s.offset = s.cursor - maxRows + 1
	}
	return s.offset, min(s.offset+maxRows, n)
}

func (s *selector) draw(view []viewItem) {
	s.clear()

	var b strings.Builder
	title := s.title
	if s.query != "" {
		title += "  " + s.ui.Dim("/"+s.query)
	}
	fmt.Fprintf(&b, "%s\r\n", title)

	start, end := s.window(len(view))
	for i := start; i < end; i++ {
		it := view[i]
		marker := "  "
		label := it.Label
		switch {
		case it.Disabled:
			// 变灰即可，不需要额外标记 —— 灰掉本身就是「现在不能选」。
			label = s.ui.Dim(label)
		case i == s.cursor:
			marker = s.ui.Bold("❯ ")
			label = s.ui.Bold(label)
		}
		line := marker + label
		if it.Detail != "" {
			line += "  " + s.ui.Dim(it.Detail)
		}
		if it.Note != "" {
			line += "  " + it.Note
		}
		fmt.Fprintf(&b, "%s\r\n", line)
	}

	if len(view) == 0 {
		fmt.Fprintf(&b, "  %s\r\n", s.ui.Dim(s.ui.T("无匹配项", "no matches")))
	}

	// 截断时必须说出来。
	//
	// 之前列表滚动是无声的：14 个模型在小窗口里只露出 8 个，用户不知道
	// 下面还有 —— 提示行只讲「↑↓ 移动」，没讲「移动能看到更多」。
	// 找不到想要的模型时，人会以为它不在，而不是以为要往下翻。
	if end-start < len(view) {
		fmt.Fprintf(&b, "  %s\r\n", s.ui.Dim(fmt.Sprintf(
			s.ui.T("第 %d-%d 个，共 %d 个", "showing %d-%d of %d"),
			start+1, end, len(view))))
	}

	// 提示行跟着状态变：正在过滤时 esc 的含义是清掉过滤，写成「取消」会骗人。
	esc := s.opt.CancelHint
	if esc == "" {
		esc = s.ui.T("取消", "cancel")
	}
	if s.query != "" {
		esc = s.ui.T("清掉过滤", "clear filter")
	}
	fmt.Fprintf(&b, "%s\r\n", s.ui.Dim(fmt.Sprintf(s.ui.T(
		"↑↓ 移动   enter 确认   esc %s   直接输入可过滤",
		"↑↓ move   enter select   esc %s   type to filter"), esc)))

	s.drawn = strings.Count(b.String(), "\n")
	fmt.Fprint(s.tty.f, b.String())
}

// clear 擦掉上一次绘制，避免刷屏时留下残影。
func (s *selector) clear() {
	if s.drawn == 0 {
		return
	}
	fmt.Fprintf(s.tty.f, "\033[%dA\033[J", s.drawn)
	s.drawn = 0
}
