package books

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var supportedSourceFormats = []string{"epub", "mobi", "azw", "azw3", "txt", "pdf", "html", "htm"}

var ErrConversionUnavailable = errors.New("book conversion is unavailable")

type ConversionStatus struct {
	Engine           string   `json:"engine"`
	Available        bool     `json:"available"`
	SupportedFormats []string `json:"supportedFormats"`
}

type Converter interface {
	Convert(ctx context.Context, sourceFilename string, source []byte) ([]byte, error)
	Status() ConversionStatus
}

type CalibreConverter struct {
	binary string
}

func NewCalibreConverter(binary string) *CalibreConverter {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		binary = "ebook-convert"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return &CalibreConverter{}
	}
	return &CalibreConverter{binary: resolved}
}

func (c *CalibreConverter) Status() ConversionStatus {
	return ConversionStatus{
		Engine:           "calibre ebook-convert",
		Available:        c != nil && c.binary != "",
		SupportedFormats: append([]string(nil), supportedSourceFormats...),
	}
}

func (c *CalibreConverter) Convert(ctx context.Context, sourceFilename string, source []byte) ([]byte, error) {
	if c == nil || c.binary == "" {
		return nil, fmt.Errorf("%w: install Calibre or configure OMNIREADER_EBOOK_CONVERT", ErrConversionUnavailable)
	}
	sourceFormat := sourceFormat(sourceFilename)
	if !isSupportedSourceFormat(sourceFormat) || sourceFormat == "epub" {
		return nil, fmt.Errorf("cannot convert source format %q", sourceFormat)
	}

	tempDir, err := os.MkdirTemp("", "omnireader-convert-*")
	if err != nil {
		return nil, fmt.Errorf("create conversion directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	inputPath := filepath.Join(tempDir, filepath.Base(sourceFilename))
	outputPath := filepath.Join(tempDir, "converted.epub")
	if err := os.WriteFile(inputPath, source, 0o600); err != nil {
		return nil, fmt.Errorf("write conversion input: %w", err)
	}

	cmd := exec.CommandContext(ctx, c.binary, inputPath, outputPath)
	cmd.Env = append(os.Environ(), "HOME="+tempDir, "TMPDIR="+tempDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 4096 {
			message = message[len(message)-4096:]
		}
		if message == "" {
			return nil, fmt.Errorf("convert %s to epub: %w", sourceFormat, err)
		}
		return nil, fmt.Errorf("convert %s to epub: %w: %s", sourceFormat, err, message)
	}

	file, err := os.Open(outputPath)
	if err != nil {
		return nil, fmt.Errorf("open converted epub: %w", err)
	}
	defer file.Close()
	converted, err := io.ReadAll(io.LimitReader(file, MaxEPUBSize+1))
	if err != nil {
		return nil, fmt.Errorf("read converted epub: %w", err)
	}
	if len(converted) == 0 {
		return nil, errors.New("converter produced an empty epub")
	}
	if len(converted) > MaxEPUBSize {
		return nil, errors.New("converted epub exceeds 64 MB limit")
	}
	return converted, nil
}

func sourceFormat(filename string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
}

func isSupportedSourceFormat(format string) bool {
	for _, supported := range supportedSourceFormats {
		if format == supported {
			return true
		}
	}
	return false
}
