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
}

// Select 展示一个可用方向键操作的列表，返回选中项下标。
//
// 无法进入 raw 模式时自动降级为编号选择器 —— 交互降级永远比报错好。
func (u *UI) Select(title string, items []Item) (int, error) {
	if len(items) == 0 {
		return 0, Errf(CodeInternal, "no items to select")
	}

	tty, err := openRawTTY()
	if err != nil {
		labels := make([]string, len(items))
		for i, it := range items {
			labels[i] = strings.TrimSpace(it.Label + "  " + it.Detail)
		}
		return u.Choose(title, labels)
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

	s := &selector{ui: u, tty: tty, title: title, all: items}
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
}

func (s *selector) run() (int, error) {
	for {
		view := s.filtered()
		if s.cursor >= len(view) {
			s.cursor = max(0, len(view)-1)
		}
		s.draw(view)

		k, r := s.tty.readKey()
		switch k {
		case keyUp:
			if s.cursor > 0 {
				s.cursor--
			}
		case keyDown:
			if s.cursor < len(view)-1 {
				s.cursor++
			}
		case keyEnter:
			if len(view) == 0 {
				continue
			}
			s.clear()
			return view[s.cursor].index, nil
		case keyCancel:
			s.clear()
			return 0, Errf(CodeCancelled, s.ui.T("已取消", "cancelled"))
		case keyBackspace:
			if s.query != "" {
				s.query = s.query[:len(s.query)-1]
				s.cursor = 0
			}
		case keyRune:
			s.query += string(r)
			s.cursor = 0
		}
	}
}

type viewItem struct {
	Item
	index int
}

// filtered 按输入的子串过滤。列表长时这是必需的，短时也无害。
func (s *selector) filtered() []viewItem {
	out := make([]viewItem, 0, len(s.all))
	q := strings.ToLower(s.query)
	for i, it := range s.all {
		if q == "" || strings.Contains(strings.ToLower(it.Label), q) {
			out = append(out, viewItem{Item: it, index: i})
		}
	}
	return out
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
		if i == s.cursor {
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

	fmt.Fprintf(&b, "%s\r\n", s.ui.Dim(s.ui.T(
		"↑↓ 移动   enter 确认   esc 取消   直接输入可过滤",
		"↑↓ move   enter select   esc cancel   type to filter")))

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
