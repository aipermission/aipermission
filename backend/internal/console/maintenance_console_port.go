package console

import "github.com/gorilla/websocket"

// MaintenanceConsoleDescriptor describes the local maintenance terminal
// without exposing its process implementation to the HTTP composition layer.
type MaintenanceConsoleDescriptor struct {
	Supported          bool
	Shell              string
	MaxInputBytes      int
	MaxTranscriptBytes int
}

type MaintenanceConsoleSnapshot struct {
	Status     string
	Shell      string
	Transcript string
}

// MaintenanceConsoleRuntime is the process-neutral port used by the gateway.
type MaintenanceConsoleRuntime interface {
	Descriptor() MaintenanceConsoleDescriptor
	Snapshot() (MaintenanceConsoleSnapshot, bool)
	Open() (MaintenanceConsoleSnapshot, error)
	Active() bool
	Attach(*websocket.Conn)
	Close() bool
}
