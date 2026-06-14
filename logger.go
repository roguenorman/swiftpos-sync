package main

import (
	"fmt"
	"os"
	"time"
)

func setupLogger(path string, maxSize int64) (*os.File, error) {
	fi, err := os.Stat(path)
	if err == nil && fi.Size() > maxSize {
		_ = os.Remove(path) 
	}
	
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	
	os.Stdout = file
	os.Stderr = file
	return file, nil
}

func logEvent(level string, message string) {
	fmt.Printf("[%s] [%s] %s\n", time.Now().UTC().Format(time.RFC3339), level, message)
}
