package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"laim/pkg/logs"
	"laim/pkg/server"
)

func main() {
	// open log file
	f, err := os.OpenFile(logs.LogFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}
	// set logger to multiwriter (colored terminal + plain file)
	log.SetOutput(io.MultiWriter(logs.ColorWriter{W: os.Stdout}, f))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// preload tail
	logs.PreloadTail(logs.LogFilePath)

	log.Println("[INFO] === Server starting ===")
	log.Println("[INFO] Logging to", logs.LogFilePath)

	// start rotation checker
	logs.StartRotation()

	// register routes
	server.RegisterRoutes()

	fmt.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
