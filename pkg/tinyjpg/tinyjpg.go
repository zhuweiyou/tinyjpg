// Package tinyjpg 调用 TinyJPG 的网页压缩接口，对 JPEG、PNG、WebP 图片做有损压缩。
package tinyjpg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// MaxFileSize 是免费接口的单张图片上限。
	MaxFileSize = 5 * 1024 * 1024

	uploadEndpoint = "https://tinyjpg.com/backend/opt/shrink"
)

// ErrTooLarge 表示图片超过免费接口的大小限制。
var ErrTooLarge = errors.New("image too large for free tier")

// ProgressFunc 接收上传/下载进度；total 为 0 时表示长度未知。
type ProgressFunc func(stage string, done, total int64)

// Options 控制单次压缩；零值可用。
type Options struct {
	HTTPClient *http.Client
	Progress   ProgressFunc
}

// Result 是压缩结果。
type Result struct {
	Data       []byte
	InputSize  int64
	OutputSize int64
}

// Compress 把图片数据上传到 TinyJPG 并返回压缩后的数据。
// contentType 例如 image/png；返回内容会校验与输入格式一致。
func Compress(ctx context.Context, data []byte, contentType string, opts *Options) (Result, error) {
	if len(data) == 0 {
		return Result{}, errors.New("empty image")
	}
	if len(data) > MaxFileSize {
		return Result{}, fmt.Errorf("%w: %d bytes (max %d)", ErrTooLarge, len(data), MaxFileSize)
	}

	contentType = normalizeContentType(contentType)
	client := &http.Client{Timeout: 5 * time.Minute}
	var progress ProgressFunc
	if opts != nil {
		if opts.HTTPClient != nil {
			client = opts.HTTPClient
		}
		progress = opts.Progress
	}

	// 上传
	var body io.Reader = bytes.NewReader(data)
	if progress != nil {
		body = &progressReader{reader: body, total: int64(len(data)), fn: progress}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadEndpoint, body)
	if err != nil {
		return Result{}, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://tinyjpg.com")
	req.Header.Set("Referer", "https://tinyjpg.com/")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.ContentLength = int64(len(data))

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, fmt.Errorf("read upload response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		var payload struct {
			Message string `json:"message"`
		}
		message := fmt.Sprintf("server returned %d", resp.StatusCode)
		if json.Unmarshal(rawResp, &payload) == nil && payload.Message != "" {
			message = payload.Message
		}
		return Result{}, fmt.Errorf("upload rejected: %s", message)
	}

	var payload struct {
		Output struct {
			URL string `json:"url"`
		} `json:"output"`
	}
	if err := json.Unmarshal(rawResp, &payload); err != nil {
		return Result{}, fmt.Errorf("parse upload response: %w", err)
	}
	if payload.Output.URL == "" {
		return Result{}, errors.New("upload response missing output URL")
	}

	// 下载压缩结果
	downloadReq, err := http.NewRequestWithContext(ctx, http.MethodGet, payload.Output.URL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("create download request: %w", err)
	}
	downloadResp, err := client.Do(downloadReq)
	if err != nil {
		return Result{}, fmt.Errorf("download: %w", err)
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("download failed: HTTP %d", downloadResp.StatusCode)
	}

	var output bytes.Buffer
	if progress != nil {
		writer := &progressWriter{writer: &output, total: downloadResp.ContentLength, fn: progress}
		if _, err := io.Copy(writer, downloadResp.Body); err != nil {
			return Result{}, fmt.Errorf("download: %w", err)
		}
	} else if _, err := io.Copy(&output, downloadResp.Body); err != nil {
		return Result{}, fmt.Errorf("download: %w", err)
	}
	if output.Len() == 0 {
		return Result{}, errors.New("empty compressed image")
	}
	if err := validateFormat(contentType, output.Bytes()); err != nil {
		return Result{}, err
	}

	return Result{
		Data:       output.Bytes(),
		InputSize:  int64(len(data)),
		OutputSize: int64(output.Len()),
	}, nil
}

func normalizeContentType(contentType string) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func validateFormat(contentType string, data []byte) error {
	switch contentType {
	case "image/png":
		if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
			return errors.New("compressed output is not PNG")
		}
	case "image/jpeg":
		if len(data) < 3 || data[0] != 0xFF || data[1] != 0xD8 || data[2] != 0xFF {
			return errors.New("compressed output is not JPEG")
		}
	case "image/webp":
		if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
			return errors.New("compressed output is not WebP")
		}
	}
	return nil
}

type progressReader struct {
	reader io.Reader
	total  int64
	done   int64
	fn     ProgressFunc
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.done += int64(n)
		r.fn("upload", r.done, r.total)
	}
	return n, err
}

type progressWriter struct {
	writer io.Writer
	total  int64
	done   int64
	fn     ProgressFunc
}

func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if n > 0 {
		w.done += int64(n)
		w.fn("download", w.done, w.total)
	}
	return n, err
}
