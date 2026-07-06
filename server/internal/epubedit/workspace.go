package epubedit

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultMaxEntries       = 2048
	defaultMaxExpandedBytes = int64(256 << 20)
	defaultMaxEntryBytes    = int64(32 << 20)
)

type Limits struct {
	MaxEntries       int
	MaxExpandedBytes int64
	MaxEntryBytes    int64
}

type Metadata struct {
	Title       string `json:"title"`
	Author      string `json:"author"`
	Language    string `json:"language"`
	Publisher   string `json:"publisher"`
	Description string `json:"description"`
	Identifier  string `json:"identifier"`
}

type Chapter struct {
	ID        string `json:"id"`
	Href      string `json:"href"`
	Title     string `json:"title"`
	MediaType string `json:"mediaType"`
}

type Content struct {
	Metadata Metadata  `json:"metadata"`
	Chapters []Chapter `json:"chapters"`
}

type manifestItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type packageDocument struct {
	Version  string `xml:"version,attr"`
	Metadata struct {
		Meta []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest []manifestItem `xml:"manifest>item"`
	Spine    []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

type containerDocument struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type Workspace struct {
	root    string
	opfPath string
	content Content
	limits  Limits
	closed  bool
}

func Open(data []byte, limits Limits) (*Workspace, error) {
	limits = normalizedLimits(limits)
	root, err := os.MkdirTemp("", "omnireader-epub-")
	if err != nil {
		return nil, fmt.Errorf("create EPUB workspace: %w", err)
	}
	workspace := &Workspace{root: root, limits: limits}
	if err := workspace.extract(data); err != nil {
		_ = workspace.Close()
		return nil, err
	}
	if err := workspace.inspect(); err != nil {
		_ = workspace.Close()
		return nil, err
	}
	return workspace, nil
}

func normalizedLimits(value Limits) Limits {
	if value.MaxEntries <= 0 {
		value.MaxEntries = defaultMaxEntries
	}
	if value.MaxExpandedBytes <= 0 {
		value.MaxExpandedBytes = defaultMaxExpandedBytes
	}
	if value.MaxEntryBytes <= 0 {
		value.MaxEntryBytes = defaultMaxEntryBytes
	}
	return value
}

func (w *Workspace) Root() string     { return w.root }
func (w *Workspace) Content() Content { return w.content }

func (w *Workspace) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return os.RemoveAll(w.root)
}

func (w *Workspace) extract(data []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return invalid("open EPUB archive", err)
	}
	if len(reader.File) > w.limits.MaxEntries {
		return invalid("EPUB has too many entries", nil)
	}
	seen := make(map[string]struct{}, len(reader.File))
	var expanded int64
	for _, entry := range reader.File {
		name, err := safeArchivePath(entry.Name)
		if err != nil {
			return err
		}
		if _, ok := seen[name]; ok {
			return invalid("EPUB contains duplicate entries", nil)
		}
		seen[name] = struct{}{}
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return invalid("EPUB contains a symbolic link", nil)
		}
		size := int64(entry.UncompressedSize64)
		if size > w.limits.MaxEntryBytes || expanded > w.limits.MaxExpandedBytes-size {
			return invalid("EPUB expanded size exceeds limit", nil)
		}
		expanded += size
		target := filepath.Join(w.root, filepath.FromSlash(name))
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create EPUB directory: %w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create EPUB entry parent: %w", err)
		}
		body, err := entry.Open()
		if err != nil {
			return invalid("open EPUB entry", err)
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			_ = body.Close()
			return fmt.Errorf("create EPUB entry: %w", err)
		}
		written, copyErr := io.Copy(file, io.LimitReader(body, w.limits.MaxEntryBytes+1))
		closeErr := file.Close()
		_ = body.Close()
		if copyErr != nil || closeErr != nil || written != size || written > w.limits.MaxEntryBytes {
			return invalid("read EPUB entry", errors.Join(copyErr, closeErr))
		}
	}
	return nil
}

func safeArchivePath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", invalid("unsafe EPUB entry path", nil)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != strings.TrimSuffix(name, "/") {
		return "", invalid("unsafe EPUB entry path", nil)
	}
	return clean, nil
}

func (w *Workspace) inspect() error {
	container, err := os.ReadFile(filepath.Join(w.root, "META-INF", "container.xml"))
	if err != nil {
		return invalid("read container.xml", err)
	}
	var parsedContainer containerDocument
	if err := decodeXML(container, &parsedContainer); err != nil {
		return invalid("parse container.xml", err)
	}
	if len(parsedContainer.Rootfiles) == 0 {
		return invalid("EPUB package document not found", nil)
	}
	opfPath, err := safeReference("", parsedContainer.Rootfiles[0].FullPath)
	if err != nil {
		return err
	}
	w.opfPath = opfPath
	opf, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(opfPath)))
	if err != nil {
		return invalid("read package document", err)
	}
	var pkg packageDocument
	if err := decodeXML(opf, &pkg); err != nil {
		return invalid("parse package document", err)
	}
	metadata, err := inspectMetadata(opf)
	if err != nil {
		return err
	}
	items := make(map[string]manifestItem, len(pkg.Manifest))
	for _, item := range pkg.Manifest {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Href) == "" {
			return invalid("manifest item requires id and href", nil)
		}
		if _, exists := items[item.ID]; exists {
			return invalid("duplicate manifest id", nil)
		}
		items[item.ID] = item
	}
	if len(pkg.Spine) == 0 {
		return invalid("EPUB spine is empty", nil)
	}
	content := Content{Metadata: metadata}
	for _, ref := range pkg.Spine {
		item, ok := items[ref.IDRef]
		if !ok {
			return invalid("spine references a missing manifest item", nil)
		}
		resource, err := safeReference(path.Dir(opfPath), item.Href)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(resource)))
		if err != nil {
			return invalid("read spine resource", err)
		}
		if item.MediaType != "application/xhtml+xml" {
			return invalid("spine resource is not XHTML", nil)
		}
		title, err := inspectXHTML(body, resource)
		if err != nil {
			return err
		}
		content.Chapters = append(content.Chapters, Chapter{ID: item.ID, Href: item.Href, Title: title, MediaType: item.MediaType})
	}
	w.content = content
	return nil
}

func decodeXML(data []byte, target any) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	return decoder.Decode(target)
}

func inspectMetadata(data []byte) (Metadata, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var result Metadata
	var inMetadata bool
	var field string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return Metadata{}, invalid("parse package metadata", err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return Metadata{}, invalid("XML directives are not allowed", nil)
		case xml.StartElement:
			if value.Name.Local == "metadata" {
				inMetadata = true
				continue
			}
			if inMetadata {
				field = value.Name.Local
			}
		case xml.EndElement:
			if value.Name.Local == "metadata" {
				inMetadata = false
			}
			if value.Name.Local == field {
				field = ""
			}
		case xml.CharData:
			if !inMetadata || field == "" {
				continue
			}
			text := strings.TrimSpace(string(value))
			if text == "" {
				continue
			}
			switch field {
			case "title":
				if result.Title == "" {
					result.Title = text
				}
			case "creator":
				if result.Author == "" {
					result.Author = text
				}
			case "language":
				if result.Language == "" {
					result.Language = text
				}
			case "publisher":
				if result.Publisher == "" {
					result.Publisher = text
				}
			case "description":
				if result.Description == "" {
					result.Description = text
				}
			case "identifier":
				if result.Identifier == "" {
					result.Identifier = text
				}
			}
		}
	}
}

func inspectXHTML(data []byte, resource string) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var capture string
	var title string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return title, nil
		}
		if err != nil {
			return "", invalid("parse XHTML", err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return "", invalid("XHTML directives are not allowed", nil)
		case xml.StartElement:
			if strings.EqualFold(value.Name.Local, "script") {
				return "", invalid("executable XHTML is not allowed", nil)
			}
			for _, attr := range value.Attr {
				if strings.HasPrefix(strings.ToLower(attr.Name.Local), "on") {
					return "", invalid("event handler attributes are not allowed", nil)
				}
				if attr.Name.Local == "href" || attr.Name.Local == "src" {
					if err := validateXHTMLReference(resource, attr.Value); err != nil {
						return "", err
					}
				}
			}
			if value.Name.Local == "title" || (title == "" && value.Name.Local == "h1") {
				capture = value.Name.Local
			}
		case xml.EndElement:
			if value.Name.Local == capture {
				capture = ""
			}
		case xml.CharData:
			if capture != "" && strings.TrimSpace(string(value)) != "" && title == "" {
				title = strings.TrimSpace(string(value))
			}
		}
	}
}

func validateXHTMLReference(resource, reference string) error {
	reference = strings.TrimSpace(reference)
	if reference == "" || strings.HasPrefix(reference, "#") {
		return nil
	}
	parsed, err := url.Parse(reference)
	if err != nil {
		return invalid("unsafe XHTML reference", err)
	}
	if parsed.Scheme != "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "mailto":
			return nil
		default:
			return invalid("unsafe XHTML reference scheme", nil)
		}
	}
	if parsed.Host != "" || strings.HasPrefix(parsed.Path, "/") {
		return invalid("unsafe XHTML reference", nil)
	}
	decoded, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return invalid("unsafe XHTML reference", err)
	}
	resolved := path.Clean(path.Join(path.Dir(resource), decoded))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return invalid("XHTML reference escapes archive root", nil)
	}
	return nil
}

func safeReference(base, href string) (string, error) {
	if strings.Contains(href, "\\") {
		return "", invalid("unsafe EPUB reference", nil)
	}
	parsed, err := url.Parse(href)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" {
		return "", invalid("unsafe EPUB reference", err)
	}
	decoded, err := url.PathUnescape(parsed.Path)
	if err != nil || decoded == "" || strings.HasPrefix(decoded, "/") {
		return "", invalid("unsafe EPUB reference", err)
	}
	joined := path.Clean(path.Join(base, decoded))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", invalid("EPUB reference escapes archive root", nil)
	}
	return joined, nil
}

func (w *Workspace) Rebuild() ([]byte, error) {
	if w.closed {
		return nil, errors.New("EPUB workspace is closed")
	}
	var names []string
	err := filepath.WalkDir(w.root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(w.root, filename)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate EPUB workspace: %w", err)
	}
	sort.Strings(names)
	if !contains(names, "mimetype") {
		return nil, invalid("mimetype is missing", nil)
	}
	ordered := []string{"mimetype"}
	for _, name := range names {
		if name != "mimetype" {
			ordered = append(ordered, name)
		}
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range ordered {
		body, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(name)))
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if name == "mimetype" {
			header.Method = zip.Store
		}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		if _, err := entry.Write(body); err != nil {
			_ = writer.Close()
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	reopened, err := Open(output.Bytes(), w.limits)
	if err != nil {
		return nil, fmt.Errorf("revalidate rebuilt EPUB: %w", err)
	}
	_ = reopened.Close()
	return output.Bytes(), nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type InvalidContentError struct {
	Message string
	Cause   error
}

func (e *InvalidContentError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}
func (e *InvalidContentError) Unwrap() error { return e.Cause }
func invalid(message string, cause error) error {
	return &InvalidContentError{Message: message, Cause: cause}
}
