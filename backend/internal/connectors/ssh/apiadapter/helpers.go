package apiadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
)

func decodeDraftRequest(value any) (draftTargetRequest, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return draftTargetRequest{}, fmt.Errorf("invalid connector draft request")
	}
	var request draftTargetRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return draftTargetRequest{}, fmt.Errorf("invalid connector draft request")
	}
	return request, nil
}

func operationProfileID(ctx context.Context, store *connectortargets.Store, targetID int64, requestedProfileID int64) (int64, error) {
	if requestedProfileID > 0 {
		return requestedProfileID, nil
	}
	profiles, err := store.ListCredentialProfiles(ctx, targetID)
	if err != nil {
		return 0, err
	}
	if len(profiles) == 0 {
		return 0, connectortargets.ErrTargetProfileNotFound
	}
	if len(profiles) > 1 {
		return 0, connectortargets.ValidationError("profile_id is required when an SSH connector target has multiple credential profiles")
	}
	return profiles[0].ID, nil
}

func execOutput(result console.ExecResult) map[string]any {
	return map[string]any{
		"command":     result.Command,
		"stdout":      console.PlainOutput(result.Output),
		"stderr":      "",
		"exit_code":   result.ExitCode,
		"running":     result.Running,
		"session_id":  result.SessionID,
		"duration_ms": result.DurationMS,
	}
}

func stringPayload(payload map[string]any, name string) string {
	value, ok := payload[name]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func intPayload(payload map[string]any, name string, fallback int) int {
	value, ok := payload[name]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func stringSlicePayload(payload map[string]any, name string) []string {
	value, ok := payload[name]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		return []string{fmt.Sprint(typed)}
	}
}

func browseParent(remotePath string) string {
	if remotePath == "" || remotePath == "/" || remotePath == "." {
		return "/"
	}
	parent := path.Dir(remotePath)
	if parent == "." {
		return "/"
	}
	return parent
}

func normalizeRemoteDirectoryPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("path cannot contain control characters")
	}
	return value, nil
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeInternalError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func handleTargetError(w http.ResponseWriter, err error) {
	var validation connectortargets.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, connectortargets.ErrTargetNotFound), errors.Is(err, connectortargets.ErrTargetProfileNotFound):
		writeError(w, http.StatusNotFound, "connector target profile not found")
	default:
		writeInternalError(w)
	}
}

func handleKeyError(w http.ResponseWriter, err error) {
	var validation sshkeys.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, validation.Error())
	case errors.Is(err, sshkeys.ErrNotFound):
		writeError(w, http.StatusNotFound, "ssh key not found")
	default:
		writeInternalError(w)
	}
}

func handleMaterialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, connectortargets.ErrTargetProfileNotFound), errors.Is(err, connectortargets.ErrTargetNotFound):
		writeError(w, http.StatusNotFound, "connector target profile not found")
	case errors.Is(err, sshkeys.ErrNotFound):
		handleKeyError(w, err)
	default:
		writeInternalError(w)
	}
}

func connectionFailureMessage(err error) string {
	return failureMessage("server connection test failed", err)
}

func commandFailureMessage(err error) string {
	return failureMessage("command execution failed", err)
}

func failureMessage(prefix string, err error) string {
	detail := safeErrorDetail(err)
	switch {
	case detail == "":
		return prefix
	case strings.Contains(detail, "unable to authenticate") || strings.Contains(detail, "no supported methods remain") || strings.Contains(detail, "permission denied"):
		return prefix + ": SSH authentication failed. Install the selected SSH public key on the server, then try again."
	case strings.Contains(detail, "connection refused"):
		return prefix + ": SSH port refused the connection. Check the host, port, and SSH service."
	case strings.Contains(detail, "i/o timeout") || strings.Contains(detail, "timed out") || strings.Contains(detail, "deadline exceeded"):
		return prefix + ": SSH connection timed out. Check network access, firewall rules, host, and port."
	case strings.Contains(detail, "no route to host") || strings.Contains(detail, "network is unreachable"):
		return prefix + ": the host is not reachable from the local gateway."
	case strings.Contains(detail, "host key"):
		return prefix + ": SSH host key verification failed."
	case strings.Contains(detail, "parse private key"):
		return prefix + ": selected SSH key could not be parsed."
	default:
		return prefix + ": " + detail
	}
}

func safeErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(err.Error()))
}

func writeUnknownHostKeyError(w http.ResponseWriter, err error) bool {
	var unknown *execution.UnknownHostKeyError
	if errors.As(err, &unknown) {
		writeHostKeyConflict(w, "ssh host key approval required", "unknown_ssh_host_key", unknownHostKeyDTOFromUnknown(unknown))
		return true
	}
	var changed *execution.ChangedHostKeyError
	if errors.As(err, &changed) {
		writeHostKeyConflict(w, "ssh host key changed; replace trusted fingerprint only if this change is expected", "changed_ssh_host_key", unknownHostKeyDTOFromChanged(changed))
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

func unknownHostKeyDTOFromUnknown(err *execution.UnknownHostKeyError) unknownHostKeyDTO {
	host, port := splitHostKeyHostPort(err.Hostname)
	return unknownHostKeyDTO{
		Host:              host,
		Port:              port,
		Hostname:          err.Hostname,
		KeyType:           err.KeyType,
		FingerprintSHA256: err.FingerprintSHA256,
		PublicKey:         err.PublicKey,
	}
}

func unknownHostKeyDTOFromChanged(err *execution.ChangedHostKeyError) unknownHostKeyDTO {
	host, port := splitHostKeyHostPort(err.Hostname)
	return unknownHostKeyDTO{
		Host:                 host,
		Port:                 port,
		Hostname:             err.Hostname,
		KeyType:              err.KeyType,
		FingerprintSHA256:    err.FingerprintSHA256,
		PublicKey:            err.PublicKey,
		Changed:              true,
		ExistingFingerprints: err.ExistingFingerprints,
	}
}

func splitHostKeyHostPort(hostname string) (string, int) {
	host, portText, splitErr := net.SplitHostPort(hostname)
	if splitErr != nil {
		return hostname, 22
	}
	port, parseErr := strconv.Atoi(portText)
	if parseErr != nil || port < 1 {
		port = 22
	}
	return host, port
}

func validateHost(host string) error {
	if len([]rune(host)) > 255 {
		return connectortargets.ValidationError("host must be 255 characters or fewer")
	}
	if strings.Contains(host, "://") || strings.ContainsAny(host, "/\\") {
		return connectortargets.ValidationError("host must be a hostname or IP address, not a URL")
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return connectortargets.ValidationError("host cannot contain whitespace")
	}
	for _, r := range host {
		if unicode.IsControl(r) {
			return connectortargets.ValidationError("host cannot contain control characters")
		}
	}
	return nil
}

func validateDockerContainerRef(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("container is required")
	}
	if len(value) > 128 {
		return fmt.Errorf("container must be 128 characters or fewer")
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\x00' {
			return fmt.Errorf("container cannot contain control characters")
		}
	}
	return nil
}

func normalizeDockerLogsTail(value int) int {
	if value <= 0 {
		return 300
	}
	if value > 5000 {
		return 5000
	}
	return value
}

func parseDockerPSOutput(output string) ([]dockerContainerState, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return []dockerContainerState{}, true
	}
	if strings.Contains(output, "__AIPERMISSION_DOCKER_UNAVAILABLE__") {
		return []dockerContainerState{}, false
	}
	containers := []dockerContainerState{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var parsed dockerPSLine
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			continue
		}
		containers = append(containers, dockerContainerState{
			ID:         parsed.ID,
			Name:       parsed.Names,
			Image:      parsed.Image,
			Command:    parsed.Command,
			CreatedAt:  parsed.CreatedAt,
			Status:     parsed.Status,
			State:      parsed.State,
			Ports:      parsed.Ports,
			RunningFor: parsed.RunningFor,
			Size:       parsed.Size,
			Labels:     parsed.Labels,
			Mounts:     parsed.Mounts,
			Networks:   parsed.Networks,
		})
	}
	return containers, true
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func remoteKeyAlreadyAbsent(message string) bool {
	return strings.Contains(message, "remote key uninstall removed 0 authorized_keys entries")
}

func removeAuthorizedKeyCommand(publicKey string) string {
	blob := publicKeyBlob(publicKey)
	delimiter := "__AIPERMISSION_AUTHORIZED_KEY__"
	for strings.Contains(blob, "\n"+delimiter+"\n") {
		delimiter += "_X"
	}
	return `set -e
KEY_BLOB="$(cat <<'` + delimiter + `'
` + blob + `
` + delimiter + `
)"
if [ -z "$KEY_BLOB" ]; then
  echo "remote key uninstall failed: invalid public key" >&2
  exit 1
fi
mkdir -p ~/.ssh
touch ~/.ssh/authorized_keys
chmod 700 ~/.ssh
tmp="$HOME/.ssh/authorized_keys.aipermission.$$"
awk -v key_blob="$KEY_BLOB" '
BEGIN { removed = 0 }
{
  keep = 1
  for (i = 1; i <= NF; i++) {
    if ($i == key_blob) {
      keep = 0
      removed++
      break
    }
  }
  if (keep) print
}
END { print removed > "/dev/stderr" }
' ~/.ssh/authorized_keys 2>"$tmp.count" > "$tmp"
removed="$(cat "$tmp.count" 2>/dev/null || printf '0')"
rm -f "$tmp.count"
if [ "${removed:-0}" -eq 0 ]; then
  rm -f "$tmp"
  echo "remote key uninstall removed 0 authorized_keys entries" >&2
  exit 1
fi
cat "$tmp" > ~/.ssh/authorized_keys
rm -f "$tmp"
chmod 600 ~/.ssh/authorized_keys
printf 'aipermission_key_removed=%s\n' "$removed"`
}

func publicKeyBlob(publicKey string) string {
	fields := strings.Fields(publicKey)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func parsePathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idText := strings.TrimSpace(r.PathValue("id"))
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func stringConfigValue(config map[string]any, key string) string {
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func intConfigValue(config map[string]any, key string, fallback int) int {
	value, ok := config[key]
	if !ok || value == nil {
		return fallback
	}
	parsed, ok := nativeIntValue(value)
	if !ok || parsed == 0 {
		return fallback
	}
	return parsed
}

func nativeIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		if !int64FitsNativeInt(typed) {
			return 0, false
		}
		return int(typed), true
	case float64:
		if typed != math.Trunc(typed) || !float64FitsNativeInt(typed) {
			return 0, false
		}
		return int(typed), true
	case json.Number:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed.String()), 10, strconv.IntSize)
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, strconv.IntSize)
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func int64FitsNativeInt(value int64) bool {
	if strconv.IntSize == 32 {
		return value >= -1<<31 && value <= 1<<31-1
	}
	return true
}

func float64FitsNativeInt(value float64) bool {
	if strconv.IntSize == 32 {
		return value >= float64(-1<<31) && value <= float64(1<<31-1)
	}
	return value >= float64(-1<<63) && value <= float64(1<<63-1)
}

func int64ConfigValue(config map[string]any, key string) int64 {
	value, ok := config[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}
