package logs

import (
	"io"
	"strings"
)

type ColorWriter struct {
	W io.Writer
}

func (cw ColorWriter) Write(p []byte) (n int, err error) {
	s := string(p)
	col := ""
	reset := "\033[0m"

	switch {
	case strings.Contains(s, "[ERROR]"):
		col = "\033[31m" // red
	case strings.Contains(s, "[UPLOAD]"):
		col = "\033[33m" // yellow
	case strings.Contains(s, "[OLLAMA]"):
		col = "\033[36m" // cyan
	case strings.Contains(s, "[CHAT]"):
		col = "\033[35m" // magenta
	case strings.Contains(s, "[SESSION]"):
		col = "\033[32m" // green
	case strings.Contains(s, "[MODELS]"):
		col = "\033[34m" // blue
	default:
		col = ""
	}

	if col != "" {
		p = []byte(col + s + reset)
	}
	return cw.W.Write(p)
}
