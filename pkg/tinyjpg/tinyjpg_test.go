package tinyjpg

import (
	"context"
	"errors"
	"testing"
)

func TestCompressRejectsTooLarge(t *testing.T) {
	_, err := Compress(context.Background(), make([]byte, MaxFileSize+1), "image/png", nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestValidateFormat(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nrest")
	if err := validateFormat("image/png", png); err != nil {
		t.Fatalf("valid png: %v", err)
	}
	if err := validateFormat("image/png", []byte("not-png")); err == nil {
		t.Fatal("expected error for non-PNG output")
	}

	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	if err := validateFormat("image/jpeg", jpeg); err != nil {
		t.Fatalf("valid jpeg: %v", err)
	}
	if err := validateFormat("image/jpeg", []byte("not-jpeg")); err == nil {
		t.Fatal("expected error for non-JPEG output")
	}
}

func TestNormalizeContentType(t *testing.T) {
	if got := normalizeContentType("image/png; charset=binary"); got != "image/png" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeContentType(""); got != "application/octet-stream" {
		t.Fatalf("got %q", got)
	}
}
