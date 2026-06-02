package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/execution"
	"github.com/aipermission/aipermission/backend/internal/servers"
)

type hostKeyApprovalRequest struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	PublicKey string `json:"public_key"`
	Replace   bool   `json:"replace"`
}

type hostKeyApprovalResponse struct {
	Status            string `json:"status"`
	Hostname          string `json:"hostname"`
	KeyType           string `json:"key_type"`
	FingerprintSHA256 string `json:"fingerprint_sha256"`
}

type unknownHostKeyResponse struct {
	Error   string            `json:"error"`
	Code    string            `json:"code"`
	HostKey unknownHostKeyDTO `json:"host_key"`
}

type unknownHostKeyDTO struct {
	Host                 string   `json:"host"`
	Port                 int      `json:"port"`
	Hostname             string   `json:"hostname"`
	KeyType              string   `json:"key_type"`
	FingerprintSHA256    string   `json:"fingerprint_sha256"`
	PublicKey            string   `json:"public_key"`
	Changed              bool     `json:"changed"`
	ExistingFingerprints []string `json:"existing_fingerprints,omitempty"`
}

func (s sshHostKeyHandlers) approveSSHHostKey(w http.ResponseWriter, r *http.Request) {
	var request hostKeyApprovalRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	request.Host = strings.TrimSpace(request.Host)
	if request.Port == 0 {
		request.Port = 22
	}
	if request.Host == "" {
		writeError(w, http.StatusBadRequest, "host is required")
		return
	}
	if err := servers.ValidateHost(request.Host); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.Port < 1 || request.Port > 65535 {
		writeError(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if strings.TrimSpace(request.PublicKey) == "" {
		writeError(w, http.StatusBadRequest, "public_key is required")
		return
	}
	hostname := net.JoinHostPort(request.Host, fmt.Sprintf("%d", request.Port))
	var err error
	if request.Replace {
		err = execution.ReplaceHostKey(s.knownHostsPath(), hostname, request.PublicKey)
	} else {
		err = execution.TrustHostKey(s.knownHostsPath(), hostname, request.PublicKey)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key, err := execution.ParseHostPublicKey(request.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hostKeyApprovalResponse{
		Status:            "approved",
		Hostname:          hostname,
		KeyType:           key.Type(),
		FingerprintSHA256: execution.HostKeyFingerprintSHA256(key),
	})
}

func writeUnknownHostKeyError(w http.ResponseWriter, err error) bool {
	var hostKeyErr *execution.UnknownHostKeyError
	if errors.As(err, &hostKeyErr) {
		writeHostKeyConflict(w, "ssh host key approval required", "unknown_ssh_host_key", unknownHostKeyDTOFromUnknown(hostKeyErr))
		return true
	}
	var changedHostKeyErr *execution.ChangedHostKeyError
	if errors.As(err, &changedHostKeyErr) {
		writeHostKeyConflict(w, "ssh host key changed; replace trusted fingerprint only if this change is expected", "changed_ssh_host_key", unknownHostKeyDTOFromChanged(changedHostKeyErr))
		return true
	}
	return false
}

func writeHostKeyConflict(w http.ResponseWriter, errorMessage string, code string, hostKey unknownHostKeyDTO) {
	writeJSON(w, http.StatusConflict, unknownHostKeyResponse{
		Error:   errorMessage,
		Code:    code,
		HostKey: hostKey,
	})
}

func unknownHostKeyDTOFromUnknown(hostKeyErr *execution.UnknownHostKeyError) unknownHostKeyDTO {
	host, port := splitHostKeyHostPort(hostKeyErr.Hostname)
	return unknownHostKeyDTO{
		Host:              host,
		Port:              port,
		Hostname:          hostKeyErr.Hostname,
		KeyType:           hostKeyErr.KeyType,
		FingerprintSHA256: hostKeyErr.FingerprintSHA256,
		PublicKey:         hostKeyErr.PublicKey,
	}
}

func unknownHostKeyDTOFromChanged(hostKeyErr *execution.ChangedHostKeyError) unknownHostKeyDTO {
	host, port := splitHostKeyHostPort(hostKeyErr.Hostname)
	return unknownHostKeyDTO{
		Host:                 host,
		Port:                 port,
		Hostname:             hostKeyErr.Hostname,
		KeyType:              hostKeyErr.KeyType,
		FingerprintSHA256:    hostKeyErr.FingerprintSHA256,
		PublicKey:            hostKeyErr.PublicKey,
		Changed:              true,
		ExistingFingerprints: hostKeyErr.ExistingFingerprints,
	}
}

func splitHostKeyHostPort(hostname string) (string, int) {
	host, portText, splitErr := net.SplitHostPort(hostname)
	port := 22
	if splitErr != nil {
		host = hostname
	} else if parsed, parseErr := strconv.Atoi(portText); parseErr == nil {
		port = parsed
	}
	return host, port
}

func (s *Server) knownHostsPath() string {
	return filepath.Join(filepath.Dir(s.config.DataPath), "known_hosts")
}
