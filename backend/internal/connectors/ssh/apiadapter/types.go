package apiadapter

import (
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

type LiveConsoleOptions struct {
	ForceShellCommand        string
	StartupInputAfterConnect string
	Generation               int64
	HasEnvironment           bool
	Environment              *sessionenv.Envelope
}

type sshTargetMaterial struct {
	ID                       int64
	Name                     string
	Host                     string
	Port                     int
	Username                 string
	StartupInputAfterConnect string
	ForceShellCommand        string
}

type targetOperationRequest struct {
	ProfileID    int64  `json:"profile_id,omitempty"`
	ContainerRef string `json:"container_ref,omitempty"`
	Tail         int    `json:"tail,omitempty"`
}

type dockerCheckResponse struct {
	RuntimeID  int64                  `json:"runtime_id"`
	TargetName string                 `json:"target_name"`
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

type dockerLogsResponse struct {
	RuntimeID    int64  `json:"runtime_id"`
	TargetName   string `json:"target_name"`
	ContainerRef string `json:"container_ref"`
	OK           bool   `json:"ok"`
	Command      string `json:"command"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	ExitCode     int    `json:"exit_code"`
	DurationMS   int64  `json:"duration_ms"`
	CheckedAt    string `json:"checked_at"`
}

type targetTestResponse struct {
	TargetID      int64          `json:"target_id"`
	ProfileID     int64          `json:"profile_id"`
	ConnectorKind string         `json:"connector_kind"`
	OK            bool           `json:"ok"`
	Status        string         `json:"status"`
	Message       string         `json:"message,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	DurationMS    int64          `json:"duration_ms"`
}

type draftTargetRequest struct {
	Name    string         `json:"name"`
	Config  map[string]any `json:"config,omitempty"`
	Profile map[string]any `json:"profile,omitempty"`
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
	Changed              bool     `json:"changed,omitempty"`
	ExistingFingerprints []string `json:"existing_fingerprints,omitempty"`
}

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

type parseConfigRequest struct {
	Content string `json:"content"`
}

type connectorPayloadValue struct {
	Name          string
	TargetConfig  map[string]any
	ProfileLabel  string
	ProfilePublic map[string]any
}
