package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSelectorFramesFitTerminalDimensions(t *testing.T) {
	for _, size := range [][2]int{{24, 80}, {12, 40}, {6, 20}, {4, 16}, {1, 12}, {40, 120}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s := &selector{ui: &UI{Lang: LangZH, Color: true}, title: strings.Repeat("模型选择", 30)}
			for i := 0; i < 50; i++ {
				s.all = append(s.all, Item{Label: "\x1b[1m" + strings.Repeat("中文-model-", 20) + "\x1b[0m", Detail: strings.Repeat("work ", 20)})
			}
			s.cursor = 25
			frame := s.render(s.filtered(), size[0], size[1])
			if got := strings.Count(frame, "\n"); got >= size[0] {
				t.Fatalf("%d lines overflow %d rows", got, size[0])
			}
			for _, line := range strings.Split(frame, "\n") {
				if got := ansi.StringWidth(line); got >= size[1] {
					t.Fatalf("%d columns overflow %d columns: %q", got, size[1], line)
				}
			}
			if !strings.Contains(frame, "…") {
				t.Fatal("truncation must be visible")
			}
		})
	}
}

func TestSelectorWindowRetainsSelectedItem(t *testing.T) {
	s := &selector{cursor: 49}
	start, end := s.window(50, 6)
	if start > s.cursor || end <= s.cursor || end-start > 2 {
		t.Fatalf("window=%d:%d", start, end)
	}
}
