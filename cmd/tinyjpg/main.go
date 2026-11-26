// tinyjpg 是一个调用 TinyJPG 网页压缩接口的命令行工具。
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zhuweiyou/tinyjpg/pkg/tinyjpg"
)

const maxConcurrency = 2

type imageFile struct {
	Path string
	Name string
	Size int64
}

func main() {
	input := "."
	if len(os.Args) > 1 {
		input = os.Args[1]
	}

	fmt.Printf("TinyJPG CLI - Compressing images in %s\n", input)
	fmt.Println(strings.Repeat("=", 50))

	files, err := scanInput(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Println("No image files (jpg, jpeg, png, webp) found.")
		return
	}

	var validFiles []imageFile
	for _, file := range files {
		if file.Size > tinyjpg.MaxFileSize {
			fmt.Printf("Skipping %s: file too large (%d bytes, max %d)\n",
				file.Name, file.Size, tinyjpg.MaxFileSize)
			continue
		}
		validFiles = append(validFiles, file)
	}
	if len(validFiles) == 0 {
		fmt.Println("No valid files to process.")
		return
	}

	fmt.Printf("Found %d image(s) to compress (max %d concurrent)\n\n", len(validFiles), maxConcurrency)

	var (
		sem          = make(chan struct{}, maxConcurrency)
		wg           sync.WaitGroup
		successCount int
		failCount    int
		mu           sync.Mutex
	)
	for _, file := range validFiles {
		wg.Add(1)
		sem <- struct{}{}
		go func(file imageFile) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := compressFile(file); err != nil {
				fmt.Fprintf(os.Stderr, "\nError processing %s: %v\n\n", file.Name, err)
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}
			mu.Lock()
			successCount++
			mu.Unlock()
		}(file)
	}
	wg.Wait()

	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Summary:\n")
	fmt.Printf("  Scanned:    %d image(s)\n", len(files))
	fmt.Printf("  Skipped:    %d (too large for free tier)\n", len(files)-len(validFiles))
	fmt.Printf("  Attempted:  %d\n", len(validFiles))
	fmt.Printf("  Successful: %d\n", successCount)
	fmt.Printf("  Failed:     %d\n", failCount)
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("All done!")
}

func compressFile(file imageFile) error {
	data, err := os.ReadFile(file.Path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	contentType := detectMimeType(file.Path)

	result, err := tinyjpg.Compress(context.Background(), data, contentType, &tinyjpg.Options{
		Progress: func(stage string, done, total int64) {
			if total <= 0 {
				return
			}
			fmt.Printf("\r%s %s: %d%%", stage, file.Name, int(float64(done)/float64(total)*100))
		},
	})
	if err != nil {
		return err
	}
	fmt.Printf("\r%s: %d -> %d bytes (%.1f%%)\n", file.Name, result.InputSize, result.OutputSize,
		float64(result.OutputSize)/float64(result.InputSize)*100)

	tmpPath := file.Path + ".tmp"
	if err := os.WriteFile(tmpPath, result.Data, 0o644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmpPath, file.Path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace original: %w", err)
	}
	return nil
}

func scanInput(input string) ([]imageFile, error) {
	if input == "" || input == "." {
		input = "."
	}
	info, err := os.Stat(input)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return scanImageFiles(input)
	}

	ext := strings.ToLower(filepath.Ext(input))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		return []imageFile{{Path: input, Name: filepath.Base(input), Size: info.Size()}}, nil
	default:
		return nil, fmt.Errorf("unsupported image format: %s", ext)
	}
}

func scanImageFiles(dir string) ([]imageFile, error) {
	var files []imageFile
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jpg", ".jpeg", ".png", ".webp":
			files = append(files, imageFile{Path: path, Name: filepath.Base(path), Size: info.Size()})
		}
		return nil
	})
	return files, err
}

func detectMimeType(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	mime := http.DetectContentType(buf[:n])
	if mime == "application/octet-stream" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".png":
			return "image/png"
		case ".webp":
			return "image/webp"
		}
	}
	return mime
}
