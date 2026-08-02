package mailconnector

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/quotedprintable"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-message/charset"
	"golang.org/x/net/html"
)

const (
	maxMIMEDepth      = 10
	maxMIMEParts      = 100
	maxAttachmentRows = 50
	maxFilenameBytes  = 255
)

type bodyPart struct {
	Path      []int
	Structure *imap.BodyStructure
}

type bodyWalkDecision uint8

const (
	bodyWalkContinue bodyWalkDecision = iota
	bodyWalkSkipChildren
	bodyWalkStop
)

func preferredTextParts(root *imap.BodyStructure) []bodyPart {
	var plain *bodyPart
	var htmlPart *bodyPart
	walkBodyStructure(root, nil, func(part bodyPart, _ int) bodyWalkDecision {
		structure := part.Structure
		if structure == nil {
			return bodyWalkContinue
		}
		if isAttachmentPart(structure) {
			return bodyWalkSkipChildren
		}
		if !strings.EqualFold(structure.MIMEType, "text") {
			return bodyWalkContinue
		}
		candidate := part
		if strings.EqualFold(structure.MIMESubType, "plain") && plain == nil {
			plain = &candidate
		}
		if strings.EqualFold(structure.MIMESubType, "html") && htmlPart == nil {
			htmlPart = &candidate
		}
		if plain != nil && htmlPart != nil {
			return bodyWalkStop
		}
		return bodyWalkContinue
	})
	parts := make([]bodyPart, 0, 2)
	if plain != nil {
		parts = append(parts, *plain)
	}
	if htmlPart != nil {
		parts = append(parts, *htmlPart)
	}
	return parts
}

func attachmentRows(root *imap.BodyStructure) ([]map[string]any, bool, bool, bool) {
	rows := make([]map[string]any, 0)
	encrypted := false
	signed := false
	rowLimitReached := false
	structureLimitReached := walkBodyStructure(root, nil, func(part bodyPart, _ int) bodyWalkDecision {
		structure := part.Structure
		if structure == nil {
			return bodyWalkContinue
		}
		contentType := strings.ToLower(structure.MIMEType + "/" + structure.MIMESubType)
		if contentType == "application/pkcs7-mime" || contentType == "application/pgp-encrypted" || contentType == "multipart/encrypted" {
			encrypted = true
		}
		if contentType == "application/pkcs7-signature" || contentType == "application/pgp-signature" || contentType == "multipart/signed" {
			signed = true
		}
		filename, _ := structure.Filename()
		isAttachment := isAttachmentPart(structure)
		if !isAttachment {
			return bodyWalkContinue
		}
		if len(rows) >= maxAttachmentRows {
			rowLimitReached = true
			return bodyWalkContinue
		}
		rows = append(rows, map[string]any{
			"part_id":             partID(part.Path),
			"filename":            safeFilename(filename),
			"content_type":        contentType,
			"declared_size_bytes": structure.Size,
			"decoded_size_bytes":  nil,
			"disposition":         strings.ToLower(structure.Disposition),
			"content_id":          boundedText(structure.Id, 1000),
		})
		if len(structure.Parts) > 0 {
			return bodyWalkSkipChildren
		}
		return bodyWalkContinue
	})
	return rows, encrypted, signed, rowLimitReached || structureLimitReached
}

func isAttachmentPart(structure *imap.BodyStructure) bool {
	if structure == nil {
		return false
	}
	filename, _ := structure.Filename()
	return strings.EqualFold(structure.Disposition, "attachment") || filename != "" || (!strings.EqualFold(structure.MIMEType, "text") && len(structure.Parts) == 0)
}

func walkBodyStructure(root *imap.BodyStructure, path []int, visit func(bodyPart, int) bodyWalkDecision) bool {
	count := 0
	limitReached := false
	stopped := false
	var walk func(*imap.BodyStructure, []int, int) bool
	walk = func(structure *imap.BodyStructure, currentPath []int, depth int) bool {
		if structure == nil {
			limitReached = true
			return true
		}
		if depth >= maxMIMEDepth {
			limitReached = true
			return true
		}
		if count >= maxMIMEParts {
			limitReached = true
			return false
		}
		count++
		part := bodyPart{Path: append([]int(nil), currentPath...), Structure: structure}
		switch visit(part, depth) {
		case bodyWalkStop:
			stopped = true
			return false
		case bodyWalkSkipChildren:
			return true
		}
		for index, child := range structure.Parts {
			childPath := append(append([]int(nil), currentPath...), index+1)
			if !walk(child, childPath, depth+1) && (stopped || count >= maxMIMEParts) {
				return false
			}
		}
		return true
	}
	walk(root, path, 0)
	return limitReached
}

func decodeTextPart(input io.Reader, structure *imap.BodyStructure, maxBytes int) (string, int, bool, bool, error) {
	if structure == nil {
		return "", 0, false, false, fmt.Errorf("message text part metadata is missing")
	}
	reader := io.LimitReader(input, maxWireBodyBytes+1)
	switch strings.ToLower(structure.Encoding) {
	case "base64":
		reader = base64.NewDecoder(base64.StdEncoding, reader)
	case "quoted-printable":
		reader = quotedprintable.NewReader(reader)
	case "", "7bit", "8bit", "binary":
	default:
		return "", 0, false, false, connectors.ClassifyError("unsupported_transfer_encoding", fmt.Errorf("unsupported message transfer encoding"))
	}
	charsetName := strings.TrimSpace(structure.Params["charset"])
	if charsetName != "" && !strings.EqualFold(charsetName, "utf-8") && !strings.EqualFold(charsetName, "us-ascii") {
		converted, err := charset.Reader(charsetName, reader)
		if err != nil {
			return "", 0, false, false, fmt.Errorf("unsupported message charset")
		}
		reader = converted
	}
	data, err := io.ReadAll(io.LimitReader(reader, int64(maxBytes+1)))
	if err != nil {
		return "", 0, false, false, fmt.Errorf("decode message body: %w", err)
	}
	decodedBytes := len(data)
	decodedSizeComplete := decodedBytes <= maxBytes
	truncated := !decodedSizeComplete
	if !decodedSizeComplete {
		data = data[:maxBytes]
	}
	text := strings.ToValidUTF8(string(data), "�")
	if strings.EqualFold(structure.MIMESubType, "html") {
		text, err = htmlToText(text)
		if err != nil {
			return "", decodedBytes, truncated, decodedSizeComplete, err
		}
	}
	if len(text) > maxBytes {
		text = strings.ToValidUTF8(text[:maxBytes], "")
		truncated = true
	}
	return text, decodedBytes, truncated, decodedSizeComplete, nil
}

func htmlToText(source string) (string, error) {
	document, err := html.Parse(io.LimitReader(strings.NewReader(source), maxHTMLBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("parse HTML mail body: %w", err)
	}
	var output bytes.Buffer
	writer := bufio.NewWriter(&output)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "head", "title", "script", "style", "noscript", "template", "svg", "iframe", "object", "form":
				return
			case "br", "p", "div", "li", "blockquote", "pre", "h1", "h2", "h3", "h4", "h5", "h6":
				_, _ = writer.WriteString("\n")
			}
		}
		if node.Type == html.TextNode {
			_, _ = writer.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "a":
				for _, attribute := range node.Attr {
					if attribute.Key == "href" && allowedLinkScheme(attribute.Val) {
						_, _ = writer.WriteString(" (" + attribute.Val + ")")
					}
				}
			case "p", "div", "li", "blockquote", "pre", "h1", "h2", "h3", "h4", "h5", "h6":
				_, _ = writer.WriteString("\n")
			}
		}
	}
	walk(document)
	_ = writer.Flush()
	return normalizeBodyWhitespace(output.String()), nil
}

func normalizeBodyWhitespace(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r", ""), "\n")
	result := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimRightFunc(line, unicode.IsSpace)
		if strings.TrimSpace(line) == "" {
			if blank || len(result) == 0 {
				continue
			}
			blank = true
			result = append(result, "")
			continue
		}
		blank = false
		result = append(result, line)
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

func allowedLinkScheme(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "mailto:")
}

func partID(path []int) string {
	if len(path) == 0 {
		return "1"
	}
	parts := make([]string, len(path))
	for index, item := range path {
		parts[index] = fmt.Sprint(item)
	}
	return strings.Join(parts, ".")
}

func safeFilename(value string) string {
	value = filepath.Base(strings.ReplaceAll(value, "\\", "/"))
	value = boundedText(value, maxFilenameBytes)
	if value == "." || value == "/" {
		return ""
	}
	return value
}
