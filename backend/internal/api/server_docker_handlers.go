package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/execution"
)

type dockerCheckResponse struct {
	ServerID   int64                  `json:"server_id"`
	ServerName string                 `json:"server_name"`
	Available  bool                   `json:"available"`
	OK         bool                   `json:"ok"`
	Command    string                 `json:"command"`
	Containers []dockerContainerState `json:"containers"`
	Stdout     string                 `json:"stdout"`
	Stderr     string                 `json:"stderr"`
	ExitCode   int                    `json:"exit_code"`
	DurationMS int64                  `json:"duration_ms"`
	CheckedAt  string                 `json:"checked_at"`
}

type dockerContainerState struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	Command    string `json:"command"`
	CreatedAt  string `json:"created_at"`
	Status     string `json:"status"`
	State      string `json:"state"`
	Ports      string `json:"ports"`
	RunningFor string `json:"running_for"`
	Size       string `json:"size"`
	Labels     string `json:"labels"`
	Mounts     string `json:"mounts"`
	Networks   string `json:"networks"`
}

type dockerPSLine struct {
	ID         string `json:"ID"`
	Names      string `json:"Names"`
	Image      string `json:"Image"`
	Command    string `json:"Command"`
	CreatedAt  string `json:"CreatedAt"`
	Status     string `json:"Status"`
	State      string `json:"State"`
	Ports      string `json:"Ports"`
	RunningFor string `json:"RunningFor"`
	Size       string `json:"Size"`
	Labels     string `json:"Labels"`
	Mounts     string `json:"Mounts"`
	Networks   string `json:"Networks"`
}

type dockerLogsRequest struct {
	ContainerRef string `json:"container_ref"`
	Tail         int    `json:"tail"`
}

type dockerLogsResponse struct {
	ServerID     int64  `json:"server_id"`
	ServerName   string `json:"server_name"`
	ContainerRef string `json:"container_ref"`
	OK           bool   `json:"ok"`
	Command      string `json:"command"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	ExitCode     int    `json:"exit_code"`
	DurationMS   int64  `json:"duration_ms"`
	CheckedAt    string `json:"checked_at"`
}

func (s serverConnectionHandlers) checkDocker(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	const command = `if ! command -v docker >/dev/null 2>&1; then
  printf '__AIPERMISSION_DOCKER_UNAVAILABLE__\n'
  exit 0
fi
docker ps --format '{{json .}}'`
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	server, privateKey, err := s.serverSSHMaterial(ctx, id)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}

	result, err := execution.RunCommand(ctx, s.executionTarget(server, privateKey), command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeError(w, http.StatusBadGateway, sshCommandFailureMessage(err))
		return
	}

	containers, available := parseDockerPSOutput(result.Stdout)
	runtime := s.activeRuntime()
	s.writeAudit(r.Context(), runtime, "user", nil, server.ID, "server.docker_check", map[string]any{
		"available":  available,
		"exit_code":  result.ExitCode,
		"containers": len(containers),
	})
	writeJSON(w, http.StatusOK, dockerCheckResponse{
		ServerID:   server.ID,
		ServerName: server.Name,
		Available:  available,
		OK:         available && result.ExitCode == 0,
		Command:    command,
		Containers: containers,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
	})
}

func (s serverConnectionHandlers) readDockerLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var request dockerLogsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	containerRef := strings.TrimSpace(request.ContainerRef)
	if err := validateDockerContainerRef(containerRef); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tail := normalizeDockerLogsTail(request.Tail)

	command := fmt.Sprintf(`if ! command -v docker >/dev/null 2>&1; then
  printf 'docker command is not available\n' >&2
  exit 127
fi
docker logs --tail %s --timestamps %s`, strconv.Itoa(tail), shellQuote(containerRef))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	server, privateKey, err := s.serverSSHMaterial(ctx, id)
	if err != nil {
		handleServerSSHMaterialError(w, err)
		return
	}

	result, err := execution.RunCommand(ctx, s.executionTarget(server, privateKey), command)
	if err != nil {
		if writeUnknownHostKeyError(w, err) {
			return
		}
		writeError(w, http.StatusBadGateway, sshCommandFailureMessage(err))
		return
	}

	runtime := s.activeRuntime()
	s.writeAudit(r.Context(), runtime, "user", nil, server.ID, "server.docker_logs", map[string]any{
		"container_ref": containerRef,
		"exit_code":     result.ExitCode,
		"tail":          tail,
	})
	writeJSON(w, http.StatusOK, dockerLogsResponse{
		ServerID:     server.ID,
		ServerName:   server.Name,
		ContainerRef: containerRef,
		OK:           result.ExitCode == 0,
		Command:      command,
		Stdout:       result.Stdout,
		Stderr:       result.Stderr,
		ExitCode:     result.ExitCode,
		DurationMS:   result.DurationMS,
		CheckedAt:    time.Now().UTC().Format(time.RFC3339),
	})
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

func validateDockerContainerRef(value string) error {
	if value == "" {
		return fmt.Errorf("container_ref is required")
	}
	if len(value) > 128 {
		return fmt.Errorf("container_ref is too long")
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("container_ref must be a single line")
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
