package tabs

func getPanelsWidth(width int) (int, int) {
	rightPanel := (width * 3 / 4)
	return width - rightPanel, rightPanel
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
