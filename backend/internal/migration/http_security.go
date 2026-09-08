package migration

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"mime"
	"net/http"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/localhttp"
)

const csrfHeaderName = "X-AIPermission-CSRF"

func newBrowserToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (s *Server) applySecurityHeaders(w http.ResponseWriter, nonce string) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"'")
}

func (s *Server) allowsRequest(r *http.Request) bool {
	return localhttp.IsLocalRemoteAddr(r.RemoteAddr) && localhttp.IsLocalhostHeader(r.Host)
}

func (s *Server) allowsMigrationMutation(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	referer := strings.TrimSpace(r.Header.Get("Referer"))
	if origin != "" {
		if !localhttp.IsSameOrigin(origin, r.Host) {
			return false
		}
	} else if referer == "" || !localhttp.IsSameOriginReferer(referer, r.Host) {
		return false
	}
	if site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if len(provided) != len(s.csrfToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.csrfToken)) == 1
}

func hasJSONContentType(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func (s *Server) acquireMigrationSlot() bool {
	select {
	case s.migrationSlot <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) releaseMigrationSlot() {
	<-s.migrationSlot
}
