package streamoutput

import (
	"sync"

	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

// Streams owns independent stdout/stderr text carry after exact byte redaction
// and before text policy, transcript storage or JSON framing.
type Streams struct {
	mu       sync.Mutex
	decoders [2]terminaltext.UTF8Stream
	closed   bool
}

func (streams *Streams) Write(kind int, data string, redactor *sessionenv.Redactor) string {
	streams.mu.Lock()
	defer streams.mu.Unlock()
	if streams.closed {
		return ""
	}
	if kind != 1 {
		kind = 0
	}
	bytes := []byte(data)
	if redactor != nil {
		bytes = redactor.Write(bytes)
	}
	return streams.decoders[kind].Write(bytes, false)
}

func (streams *Streams) Close(redactors [2]*sessionenv.Redactor) (output [2]string) {
	streams.mu.Lock()
	defer streams.mu.Unlock()
	if streams.closed {
		return output
	}
	streams.closed = true
	for kind, redactor := range redactors {
		var bytes []byte
		if redactor != nil {
			bytes = redactor.Close()
		}
		output[kind] = streams.decoders[kind].Write(bytes, true)
	}
	return output
}
