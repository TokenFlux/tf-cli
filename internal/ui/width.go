package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Width uses the same ANSI-aware character widths as selector truncation.
func Width(s string) int { return ansi.StringWidth(s) }

// Pad right-pads to w terminal columns without truncating longer strings.
func Pad(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-Width(s)))
}
