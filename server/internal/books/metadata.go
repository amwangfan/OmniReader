package books

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

type EPUBMetadata struct {
	Title  string
	Author string
}

type containerXML struct {
	Rootfiles []rootfileXML `xml:"rootfiles>rootfile"`
}

type rootfileXML struct {
	FullPath string `xml:"full-path,attr"`
}

func ParseEPUBMetadata(data []byte) (EPUBMetadata, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return EPUBMetadata{}, fmt.Errorf("open epub zip: %w", err)
	}

	opfPath, err := findOPFPath(reader)
	if err != nil {
		return EPUBMetadata{}, err
	}
	opf, err := readZipFile(reader, opfPath)
	if err != nil {
		return EPUBMetadata{}, err
	}
	return parseOPFMetadata(opf)
}

func findOPFPath(reader *zip.Reader) (string, error) {
	container, err := readZipFile(reader, "META-INF/container.xml")
	if err != nil {
		return "", err
	}
	var parsed containerXML
	if err := xml.Unmarshal(container, &parsed); err != nil {
		return "", fmt.Errorf("parse container.xml: %w", err)
	}
	for _, rootfile := range parsed.Rootfiles {
		if strings.TrimSpace(rootfile.FullPath) != "" {
			return rootfile.FullPath, nil
		}
	}
	return "", errors.New("epub package document not found")
}

func parseOPFMetadata(data []byte) (EPUBMetadata, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var inMetadata bool
	var current string
	var result EPUBMetadata

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return EPUBMetadata{}, fmt.Errorf("parse opf metadata: %w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "metadata" {
				inMetadata = true
				continue
			}
			if inMetadata && (value.Name.Local == "title" || value.Name.Local == "creator") {
				current = value.Name.Local
			}
		case xml.EndElement:
			if value.Name.Local == "metadata" {
				inMetadata = false
			}
			if value.Name.Local == current {
				current = ""
			}
		case xml.CharData:
			if !inMetadata || current == "" {
				continue
			}
			text := strings.TrimSpace(string(value))
			if text == "" {
				continue
			}
			if current == "title" && result.Title == "" {
				result.Title = text
			}
			if current == "creator" && result.Author == "" {
				result.Author = text
			}
		}
	}
	return result, nil
}

func readZipFile(reader *zip.Reader, name string) ([]byte, error) {
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		body, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", name, err)
		}
		defer body.Close()
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s not found", name)
}
