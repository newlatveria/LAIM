package logs

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

var (
	LogFilePath  = "server.log"
	MaxSizeBytes int64 = 10 * 1024 * 1024 // 10 MB
	MaxBackups   = 5

	logFile  *os.File
	rotateMu sync.Mutex
)

func StartRotation() {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			RotateIfNeeded()
		}
	}()
}

func RotateIfNeeded() {
	rotateMu.Lock()
	defer rotateMu.Unlock()

	fi, err := os.Stat(LogFilePath)
	if err != nil {
		// maybe not exists
		return
	}
	if fi.Size() < MaxSizeBytes {
		return
	}

	if logFile != nil {
		_ = logFile.Close()
	}

	for i := MaxBackups - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", LogFilePath, i)
		to := fmt.Sprintf("%s.%d", LogFilePath, i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	_ = os.Rename(LogFilePath, fmt.Sprintf("%s.1", LogFilePath))

	f, err := os.OpenFile(LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rotate failed: %v\n", err)
		log.SetOutput(ColorWriter{W: os.Stdout})
		return
	}
	logFile = f
	multi := io.MultiWriter(ColorWriter{W: os.Stdout}, logFile)
	log.SetOutput(multi)
	log.Println("[INFO] Rotated logs; new log file created")
}
