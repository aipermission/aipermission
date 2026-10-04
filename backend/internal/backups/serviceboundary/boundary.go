// Package serviceboundary rejects reflected backup credentials in remote
// metadata and byte streams before they enter local storage or UI projections.
package serviceboundary

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
)

var ErrReflectedCredential = errors.New("backup service response violated the credential boundary")
var ErrUnavailable = errors.New("backup service credential boundary is unavailable")

// Boundary is immutable after construction and can be shared by concurrent
// requests. Service-client input validation remains responsible for token policy.
type Boundary struct {
	patterns [][]byte
	longest  int
}

func New(token string) (*Boundary, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrUnavailable
	}
	boundary := &Boundary{}
	seen := map[string]bool{}
	for _, variant := range tokenVariants(token) {
		if variant == "" || seen[variant] {
			continue
		}
		seen[variant] = true
		boundary.patterns = append(boundary.patterns, []byte(variant))
		if len(variant) > boundary.longest {
			boundary.longest = len(variant)
		}
	}
	return boundary, nil
}

func (boundary *Boundary) CheckMetadata(value any) error {
	if boundary == nil || len(boundary.patterns) == 0 {
		return ErrUnavailable
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return errors.New("backup service returned invalid metadata")
	}
	for _, pattern := range boundary.patterns {
		if bytes.Contains(encoded.Bytes(), pattern) {
			return ErrReflectedCredential
		}
	}
	return nil
}

func (boundary *Boundary) NewScanningWriter(destination io.Writer) (*ScanningWriter, error) {
	if boundary == nil || len(boundary.patterns) == 0 {
		return nil, ErrUnavailable
	}
	if destination == nil {
		return nil, errors.New("backup service scan destination is unavailable")
	}
	return &ScanningWriter{destination: destination, patterns: boundary.patterns, maxPattern: boundary.longest}, nil
}

func tokenVariants(token string) []string {
	token = strings.TrimSpace(token)
	quoted := strconv.Quote(token)
	if len(quoted) >= 2 {
		quoted = quoted[1 : len(quoted)-1]
	}
	bearer := "Bearer " + token
	jsonQuoted, _ := json.Marshal(token)
	jsonBearer, _ := json.Marshal(bearer)
	return []string{
		token,
		quoted,
		strings.Trim(string(jsonQuoted), `"`),
		url.QueryEscape(token),
		url.PathEscape(token),
		base64.StdEncoding.EncodeToString([]byte(token)),
		base64.RawStdEncoding.EncodeToString([]byte(token)),
		base64.URLEncoding.EncodeToString([]byte(token)),
		base64.RawURLEncoding.EncodeToString([]byte(token)),
		bearer,
		strings.Trim(string(jsonBearer), `"`),
		url.QueryEscape(bearer),
		url.PathEscape(bearer),
		base64.StdEncoding.EncodeToString([]byte(bearer)),
		base64.RawStdEncoding.EncodeToString([]byte(bearer)),
		base64.URLEncoding.EncodeToString([]byte(bearer)),
		base64.RawURLEncoding.EncodeToString([]byte(bearer)),
	}
}
