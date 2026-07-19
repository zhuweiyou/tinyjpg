package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxConcurrency = 2
	maxFileSize    = 5 * 1024 * 1024 // 5MB for free tier
	uploadEndpoint = "https://tinyjpg.com/backend/opt/shrink"
)

// ImageFile represents an image file to compress.
type ImageFile struct {
	Path string
	Name string
	Size int64
}

// CompressResult holds the result of compressing a single image.
type CompressResult struct {
	OriginalPath string
	InputSize    int64
	OutputSize   int64
	DownloadURL  string
	Error        error
}

// progressReader tracks read progress through an io.Reader.
type progressReader struct {
	reader    io.Reader
	total     int64
	read      int64
	lastPrint int
	label     string
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.read += int64(n)
		percent := int(float64(r.read) / float64(r.total) * 100)
		if percent != r.lastPrint {
			r.lastPrint = percent
			fmt.Fprintf(os.Stdout, "\r%s: %d%%", r.label, percent)
		}
	}
	return n, err
}

// progressWriter tracks write progress through an io.Writer.
type progressWriter struct {
	writer    io.Writer
	total     int64
	written   int64
	lastPrint int
	label     string
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if n > 0 {
		w.written += int64(n)
		if w.total > 0 {
			percent := int(float64(w.written) / float64(w.total) * 100)
			if percent != w.lastPrint {
				w.lastPrint = percent
				fmt.Fprintf(os.Stdout, "\r%s: %d%%", w.label, percent)
			}
		} else {
			fmt.Fprintf(os.Stdout, "\r%s: %d bytes", w.label, w.written)
		}
	}
	return n, err
}

// scanImageFiles walks the given directory and returns all supported image files.
func scanImageFiles(dir string) ([]ImageFile, error) {
	var files []ImageFile
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp":
			files = append(files, ImageFile{
				Path: path,
				Name: filepath.Base(path),
				Size: info.Size(),
			})
		}
		return nil
	})
	return files, err
}

// detectMimeType detects the MIME type of a file using content sniffing
// with fallback to extension-based detection.
func detectMimeType(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	mime := http.DetectContentType(buf[:n])

	// Fallback to extension if detection returns generic type
	if mime == "application/octet-stream" {
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".png":
			mime = "image/png"
		case ".webp":
			mime = "image/webp"
		}
	}
	return mime
}

// uploadImage sends the file to TinyJPG for compression and returns the result.
func uploadImage(filePath string) (*CompressResult, error) {
	// Read file into memory (max 5MB for free tier, fine for in-memory)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %v", err)
	}

	if stat.Size() > maxFileSize {
		return &CompressResult{
			OriginalPath: filePath,
			Error:        fmt.Errorf("file too large: %d bytes (max %d for free tier)", stat.Size(), maxFileSize),
		}, nil
	}

	mimeType := detectMimeType(filePath)

	// Track upload progress using a custom reader over bytes.Reader
	pr := &progressReader{
		reader: bytes.NewReader(data),
		total:  int64(len(data)),
		label:  fmt.Sprintf("Uploading %s", filepath.Base(filePath)),
	}

	req, err := http.NewRequest("POST", uploadEndpoint, pr)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", mimeType)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://tinyjpg.com")
	req.Header.Set("Referer", "https://tinyjpg.com/")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.ContentLength = int64(len(data))

	// Send request
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		var errResp struct {
			Message string `json:"message"`
		}
		msg := fmt.Sprintf("server returned %d", resp.StatusCode)
		if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
			msg = errResp.Message
		}
		return &CompressResult{
			OriginalPath: filePath,
			Error:        fmt.Errorf("%s", msg),
		}, nil
	}

	// Parse response
	var result struct {
		Input struct {
			Size int64  `json:"size"`
			Type string `json:"type"`
		} `json:"input"`
		Output struct {
			Size  int64   `json:"size"`
			Ratio float64 `json:"ratio"`
			URL   string  `json:"url"`
		} `json:"output"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	// Download URL is the output.url directly (confirmed from actual response)
	downloadURL := result.Output.URL

	return &CompressResult{
		OriginalPath: filePath,
		InputSize:    result.Input.Size,
		OutputSize:   result.Output.Size,
		DownloadURL:  downloadURL,
	}, nil
}

// downloadImage downloads the compressed image and overwrites the original file.
func downloadImage(result *CompressResult) error {
	if result.Error != nil {
		return result.Error
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(result.DownloadURL)
	if err != nil {
		return fmt.Errorf("download request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Write to temp file first, then atomically replace original
	tmpPath := result.OriginalPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	pw := &progressWriter{
		writer: out,
		total:  resp.ContentLength,
		label:  fmt.Sprintf("Downloading %s", filepath.Base(result.OriginalPath)),
	}

	_, err = io.Copy(pw, resp.Body)
	out.Close()

	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("download failed: %v", err)
	}

	// Atomically replace original with compressed version
	if err := os.Rename(tmpPath, result.OriginalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to replace original file: %v", err)
	}

	// Print summary (progressWriter already showed 100%)
	ratio := float64(result.OutputSize) / float64(result.InputSize) * 100
	saved := result.InputSize - result.OutputSize
	fmt.Fprintf(os.Stdout, "  %s: %d -> %d bytes (%.1f%%%%, saved %d bytes)\n\n",
		filepath.Base(result.OriginalPath),
		result.InputSize, result.OutputSize,
		ratio, saved)

	return nil
}

// scanInput resolves a command line input into image files.
// Supported inputs:
//   - no argument: current directory
//   - ".": current directory
//   - directory path: recursively scan images
//   - image path: process the single image
func scanInput(input string) ([]ImageFile, error) {
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
		return []ImageFile{{
			Path: input,
			Name: filepath.Base(input),
			Size: info.Size(),
		}}, nil
	default:
		return nil, fmt.Errorf("unsupported image format: %s", ext)
	}
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
		fmt.Fprintf(os.Stderr, "Error scanning directory: %v\n", err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Println("No image files (jpg, jpeg, png, webp) found in current directory.")
		return
	}

	// Filter out files that exceed the free tier size limit
	var validFiles []ImageFile
	for _, f := range files {
		if f.Size > maxFileSize {
			fmt.Printf("Skipping %s: file too large (%d bytes, max %d for free tier)\n",
				filepath.Base(f.Path), f.Size, maxFileSize)
		} else {
			validFiles = append(validFiles, f)
		}
	}

	if len(validFiles) == 0 {
		fmt.Println("No valid files to process.")
		return
	}

	fmt.Printf("Found %d image(s) to compress (max %d concurrent)\n\n",
		len(validFiles), maxConcurrency)

	// Process files with concurrency limit of 2
	var (
		sem          = make(chan struct{}, maxConcurrency)
		wg           sync.WaitGroup
		successCount int
		failCount    int
		mu           sync.Mutex
	)

	for _, f := range validFiles {
		wg.Add(1)
		sem <- struct{}{}
		go func(f ImageFile) {
			defer wg.Done()
			defer func() { <-sem }()

			result, err := uploadImage(f.Path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nError uploading %s: %v\n\n", f.Name, err)
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}

			if result.Error != nil {
				fmt.Fprintf(os.Stderr, "\nError processing %s: %v\n\n", f.Name, result.Error)
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}

			if err := downloadImage(result); err != nil {
				fmt.Fprintf(os.Stderr, "\nError downloading %s: %v\n\n", f.Name, err)
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()
		}(f)
	}

	wg.Wait()

	// Print final summary
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
