package logs

import (
	"io/ioutil"
	"strings"
)

func PreloadTail(path string) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > TailMax {
		lines = lines[len(lines)-TailMax:]
	}
	TailMu.Lock()
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			TailLines = append(TailLines, ln)
		}
	}
	TailMu.Unlock()
}
