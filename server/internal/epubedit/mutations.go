package epubedit

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

var ErrNotFound = errors.New("EPUB resource not found")

const dcNamespace = "http://purl.org/dc/elements/1.1/"

func (w *Workspace) UpdateMetadata(metadata Metadata) error {
	values := map[string]string{
		"title": metadata.Title, "creator": metadata.Author, "language": metadata.Language,
		"publisher": metadata.Publisher, "description": metadata.Description, "identifier": metadata.Identifier,
	}
	opf, err := w.readOPF()
	if err != nil {
		return err
	}
	rewritten, err := rewriteMetadata(opf, values)
	if err != nil {
		return err
	}
	return w.writeOPFAndRefresh(rewritten)
}

func rewriteMetadata(data []byte, values map[string]string) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	inMetadata := false
	foundMetadata := false
	seen := make(map[string]bool)
	active := ""
	activeDepth := 0
	wroteValue := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, invalid("parse package metadata", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "metadata" {
				inMetadata = true
				foundMetadata = true
			}
			if inMetadata {
				if _, ok := values[value.Name.Local]; ok && active == "" {
					active = value.Name.Local
					activeDepth = 1
					wroteValue = false
					seen[active] = true
				} else if active != "" {
					activeDepth++
				}
			}
		case xml.CharData:
			if active != "" && !wroteValue {
				token = xml.CharData([]byte(values[active]))
				wroteValue = true
			} else if active != "" {
				token = xml.CharData(nil)
			}
		case xml.EndElement:
			if value.Name.Local == "metadata" {
				for _, name := range sortedMetadataFields(values) {
					if seen[name] {
						continue
					}
					start := xml.StartElement{Name: xml.Name{Space: dcNamespace, Local: name}}
					if err := encoder.EncodeToken(start); err != nil {
						return nil, err
					}
					if err := encoder.EncodeToken(xml.CharData([]byte(values[name]))); err != nil {
						return nil, err
					}
					if err := encoder.EncodeToken(start.End()); err != nil {
						return nil, err
					}
				}
				inMetadata = false
			}
			if active != "" {
				if activeDepth == 1 && !wroteValue {
					if err := encoder.EncodeToken(xml.CharData([]byte(values[active]))); err != nil {
						return nil, err
					}
					wroteValue = true
				}
				activeDepth--
				if activeDepth == 0 {
					active = ""
				}
			}
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if !foundMetadata {
		return nil, invalid("package metadata element not found", nil)
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func sortedMetadataFields(values map[string]string) []string {
	fields := make([]string, 0, len(values))
	for field := range values {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func (w *Workspace) ChapterSource(id string) (string, error) {
	chapter, err := w.chapter(id)
	if err != nil {
		return "", err
	}
	resource, err := safeReference(path.Dir(w.opfPath), chapter.Href)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(resource)))
	if err != nil {
		return "", fmt.Errorf("read chapter source: %w", err)
	}
	return string(body), nil
}

func (w *Workspace) UpdateChapter(id, source, title string) error {
	chapter, err := w.chapter(id)
	if err != nil {
		return err
	}
	resource, err := safeReference(path.Dir(w.opfPath), chapter.Href)
	if err != nil {
		return err
	}
	if err := validateXHTMLSource([]byte(source), resource); err != nil {
		return err
	}
	if strings.TrimSpace(title) != "" {
		updated, err := rewriteXHTMLTitle([]byte(source), strings.TrimSpace(title))
		if err != nil {
			return err
		}
		source = string(updated)
	}
	if err := os.WriteFile(filepath.Join(w.root, filepath.FromSlash(resource)), []byte(source), 0o644); err != nil {
		return fmt.Errorf("write chapter source: %w", err)
	}
	return w.inspect()
}

func validateXHTMLSource(data []byte, resource string) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var rootSeen bool
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return invalid("parse XHTML", err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return invalid("XHTML directives are not allowed", nil)
		case xml.StartElement:
			if !rootSeen {
				rootSeen = true
				if value.Name.Local != "html" {
					return invalid("XHTML root must be html", nil)
				}
			}
			if strings.EqualFold(value.Name.Local, "script") {
				return invalid("executable XHTML is not allowed", nil)
			}
			for _, attr := range value.Attr {
				lower := strings.ToLower(attr.Name.Local)
				if strings.HasPrefix(lower, "on") {
					return invalid("event handler attributes are not allowed", nil)
				}
				if lower == "href" || lower == "src" {
					if err := validateXHTMLReference(resource, attr.Value); err != nil {
						return err
					}
				}
			}
		}
	}
	if !rootSeen {
		return invalid("XHTML is empty", nil)
	}
	return nil
}

func rewriteXHTMLTitle(data []byte, title string) ([]byte, error) {
	escaped := html.EscapeString(title)
	titlePattern := regexp.MustCompile(`(?is)(<title(?:\s[^>]*)?>).*?(</title\s*>)`)
	if titlePattern.Match(data) {
		return titlePattern.ReplaceAll(data, []byte(`${1}`+escaped+`${2}`)), nil
	}
	headEnd := regexp.MustCompile(`(?i)</head\s*>`)
	if !headEnd.Match(data) {
		return nil, invalid("XHTML head element not found", nil)
	}
	return headEnd.ReplaceAll(data, []byte(`<title>`+escaped+`</title></head>`)), nil
}

func (w *Workspace) AddChapter(title, source string) (Chapter, error) {
	existing := make(map[string]bool)
	for _, chapter := range w.content.Chapters {
		existing[chapter.ID] = true
	}
	index := len(existing) + 1
	id := "chapter-added-" + strconv.Itoa(index)
	for existing[id] {
		index++
		id = "chapter-added-" + strconv.Itoa(index)
	}
	href := "text/added-" + strconv.Itoa(index) + ".xhtml"
	resource, err := safeReference(path.Dir(w.opfPath), href)
	if err != nil {
		return Chapter{}, err
	}
	if err := validateXHTMLSource([]byte(source), resource); err != nil {
		return Chapter{}, err
	}
	if strings.TrimSpace(title) != "" {
		updated, err := rewriteXHTMLTitle([]byte(source), strings.TrimSpace(title))
		if err != nil {
			return Chapter{}, err
		}
		source = string(updated)
	}
	filename := filepath.Join(w.root, filepath.FromSlash(resource))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return Chapter{}, err
	}
	if err := os.WriteFile(filename, []byte(source), 0o644); err != nil {
		return Chapter{}, err
	}
	opf, err := w.readOPF()
	if err != nil {
		_ = os.Remove(filename)
		return Chapter{}, err
	}
	rewritten, err := addManifestAndSpine(opf, manifestItem{ID: id, Href: href, MediaType: "application/xhtml+xml"})
	if err != nil {
		_ = os.Remove(filename)
		return Chapter{}, err
	}
	if err := w.writeOPFAndRefresh(rewritten); err != nil {
		_ = os.Remove(filename)
		return Chapter{}, err
	}
	return w.chapter(id)
}

func addManifestAndSpine(data []byte, item manifestItem) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	manifest, spine := false, false
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		if end, ok := token.(xml.EndElement); ok {
			if end.Name.Local == "manifest" {
				start := xml.StartElement{Name: xml.Name{Local: "item"}, Attr: []xml.Attr{{Name: xml.Name{Local: "id"}, Value: item.ID}, {Name: xml.Name{Local: "href"}, Value: item.Href}, {Name: xml.Name{Local: "media-type"}, Value: item.MediaType}}}
				if item.Properties != "" {
					start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "properties"}, Value: item.Properties})
				}
				_ = encoder.EncodeToken(start)
				_ = encoder.EncodeToken(start.End())
				manifest = true
			}
			if end.Name.Local == "spine" {
				start := xml.StartElement{Name: xml.Name{Local: "itemref"}, Attr: []xml.Attr{{Name: xml.Name{Local: "idref"}, Value: item.ID}}}
				_ = encoder.EncodeToken(start)
				_ = encoder.EncodeToken(start.End())
				spine = true
			}
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if !manifest || !spine {
		return nil, invalid("manifest or spine not found", nil)
	}
	_ = encoder.Flush()
	return output.Bytes(), nil
}

func (w *Workspace) DeleteChapter(id string) error {
	if len(w.content.Chapters) <= 1 {
		return invalid("cannot delete final spine item", nil)
	}
	chapter, err := w.chapter(id)
	if err != nil {
		return err
	}
	opf, err := w.readOPF()
	if err != nil {
		return err
	}
	rewritten, err := filterOPFItems(opf, id)
	if err != nil {
		return err
	}
	resource, err := safeReference(path.Dir(w.opfPath), chapter.Href)
	if err != nil {
		return err
	}
	if err := w.writeOPFAndRefresh(rewritten); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(w.root, filepath.FromSlash(resource)))
	return nil
}

func filterOPFItems(data []byte, id string) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	skipDepth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok {
			if skipDepth > 0 {
				skipDepth++
				continue
			}
			if (start.Name.Local == "item" && attribute(start, "id") == id) || (start.Name.Local == "itemref" && attribute(start, "idref") == id) {
				skipDepth = 1
				continue
			}
		}
		if _, ok := token.(xml.EndElement); ok && skipDepth > 0 {
			skipDepth--
			continue
		}
		if skipDepth > 0 {
			continue
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	_ = encoder.Flush()
	return output.Bytes(), nil
}

func (w *Workspace) ReorderSpine(ids []string) error {
	if len(ids) != len(w.content.Chapters) || len(ids) == 0 {
		return invalid("spine must contain every chapter exactly once", nil)
	}
	existing := make(map[string]bool, len(w.content.Chapters))
	for _, chapter := range w.content.Chapters {
		existing[chapter.ID] = true
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !existing[id] || seen[id] {
			return invalid("spine contains duplicate or unknown chapter", nil)
		}
		seen[id] = true
	}
	opf, err := w.readOPF()
	if err != nil {
		return err
	}
	rewritten, err := rewriteSpine(opf, ids)
	if err != nil {
		return err
	}
	return w.writeOPFAndRefresh(rewritten)
}

func rewriteSpine(data []byte, ids []string) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	inSpine, found, skipDepth := false, false, 0
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok {
			if start.Name.Local == "spine" {
				inSpine = true
				found = true
			}
			if inSpine && start.Name.Local == "itemref" {
				skipDepth = 1
				continue
			}
			if skipDepth > 0 {
				skipDepth++
				continue
			}
		}
		if end, ok := token.(xml.EndElement); ok {
			if skipDepth > 0 {
				skipDepth--
				continue
			}
			if end.Name.Local == "spine" {
				for _, id := range ids {
					start := xml.StartElement{Name: xml.Name{Local: "itemref"}, Attr: []xml.Attr{{Name: xml.Name{Local: "idref"}, Value: id}}}
					_ = encoder.EncodeToken(start)
					_ = encoder.EncodeToken(start.End())
				}
				inSpine = false
			}
		}
		if skipDepth > 0 {
			continue
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if !found {
		return nil, invalid("spine not found", nil)
	}
	_ = encoder.Flush()
	return output.Bytes(), nil
}

type CoverLimits struct {
	MaxBytes  int
	MaxWidth  int
	MaxHeight int
	MaxPixels int64
}
type CoverInfo struct {
	Data      []byte `json:"-"`
	MediaType string `json:"mediaType"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Href      string `json:"href"`
}

func (w *Workspace) ReplaceCover(data []byte, limits CoverLimits) (CoverInfo, error) {
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = 10 << 20
	}
	if limits.MaxWidth <= 0 {
		limits.MaxWidth = 10000
	}
	if limits.MaxHeight <= 0 {
		limits.MaxHeight = 10000
	}
	if limits.MaxPixels <= 0 {
		limits.MaxPixels = 40_000_000
	}
	if len(data) == 0 || len(data) > limits.MaxBytes {
		return CoverInfo{}, invalid("cover size exceeds limit", nil)
	}
	mediaType := http.DetectContentType(data)
	if mediaType != "image/jpeg" && mediaType != "image/png" && mediaType != "image/webp" {
		return CoverInfo{}, invalid("cover must be JPEG, PNG, or WebP", nil)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return CoverInfo{}, invalid("decode cover", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > limits.MaxWidth || config.Height > limits.MaxHeight || int64(config.Width) > limits.MaxPixels/int64(config.Height) {
		return CoverInfo{}, invalid("cover dimensions exceed limit", nil)
	}
	opf, err := w.readOPF()
	if err != nil {
		return CoverInfo{}, err
	}
	var pkg packageDocument
	if err := decodeXML(opf, &pkg); err != nil {
		return CoverInfo{}, err
	}
	var cover manifestItem
	for _, item := range pkg.Manifest {
		if containsWord(item.Properties, "cover-image") {
			cover = item
			break
		}
	}
	if cover.ID == "" {
		ext := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp"}[mediaType]
		cover = manifestItem{ID: "cover-image", Href: "images/cover." + ext, MediaType: mediaType, Properties: "cover-image"}
		rewritten, err := addManifestOnly(opf, cover)
		if err != nil {
			return CoverInfo{}, err
		}
		opf = rewritten
	} else {
		rewritten, err := updateManifestMediaType(opf, cover.ID, mediaType)
		if err != nil {
			return CoverInfo{}, err
		}
		opf = rewritten
	}
	resource, err := safeReference(path.Dir(w.opfPath), cover.Href)
	if err != nil {
		return CoverInfo{}, err
	}
	filename := filepath.Join(w.root, filepath.FromSlash(resource))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return CoverInfo{}, err
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return CoverInfo{}, err
	}
	if err := w.writeOPFAndRefresh(opf); err != nil {
		return CoverInfo{}, err
	}
	return CoverInfo{Data: append([]byte(nil), data...), MediaType: mediaType, Width: config.Width, Height: config.Height, Href: cover.Href}, nil
}

func addManifestOnly(data []byte, item manifestItem) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	found := false
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		if end, ok := token.(xml.EndElement); ok && end.Name.Local == "manifest" {
			start := xml.StartElement{Name: xml.Name{Local: "item"}, Attr: []xml.Attr{{Name: xml.Name{Local: "id"}, Value: item.ID}, {Name: xml.Name{Local: "href"}, Value: item.Href}, {Name: xml.Name{Local: "media-type"}, Value: item.MediaType}, {Name: xml.Name{Local: "properties"}, Value: item.Properties}}}
			_ = encoder.EncodeToken(start)
			_ = encoder.EncodeToken(start.End())
			found = true
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if !found {
		return nil, invalid("manifest not found", nil)
	}
	_ = encoder.Flush()
	return output.Bytes(), nil
}
func updateManifestMediaType(data []byte, id, mediaType string) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var output bytes.Buffer
	encoder := xml.NewEncoder(&output)
	found := false
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "item" && attribute(start, "id") == id {
			for i := range start.Attr {
				if start.Attr[i].Name.Local == "media-type" {
					start.Attr[i].Value = mediaType
					found = true
				}
			}
			token = start
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if !found {
		return nil, invalid("cover manifest item is invalid", nil)
	}
	_ = encoder.Flush()
	return output.Bytes(), nil
}

func containsWord(value, wanted string) bool {
	for _, word := range strings.Fields(value) {
		if word == wanted {
			return true
		}
	}
	return false
}
func attribute(start xml.StartElement, name string) string {
	for _, attr := range start.Attr {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}
func (w *Workspace) chapter(id string) (Chapter, error) {
	for _, chapter := range w.content.Chapters {
		if chapter.ID == id {
			return chapter, nil
		}
	}
	return Chapter{}, fmt.Errorf("%w: chapter %q", ErrNotFound, id)
}
func (w *Workspace) readOPF() ([]byte, error) {
	body, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(w.opfPath)))
	if err != nil {
		return nil, fmt.Errorf("read package document: %w", err)
	}
	return body, nil
}
func (w *Workspace) writeOPFAndRefresh(data []byte) error {
	filename := filepath.Join(w.root, filepath.FromSlash(w.opfPath))
	old, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return err
	}
	if err := w.inspect(); err != nil {
		_ = os.WriteFile(filename, old, 0o644)
		_ = w.inspect()
		return err
	}
	return nil
}
