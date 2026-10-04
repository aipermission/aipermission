package conformance_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// QEMU has no NIC, host mounts, socket or device access. Docker runs only in RAM
// inside its disposable guest, never against the orchestrator's daemon.
func TestDockerDaemonRealService(t *testing.T) {
	requireConformance(t)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	// Serialize translation while retaining both virtual CPUs and the complete
	// daemon qualification; this fixture needs no parallel translation threads.
	command := exec.CommandContext(ctx, "qemu-system-x86_64",
		"-accel", "tcg,thread=single,tb-size=64", "-cpu", "max", "-m", "1024", "-smp", "2", "-nic", "none",
		"-display", "none", "-monitor", "none", "-serial", "stdio", "-no-reboot",
		"-kernel", "/fixtures/docker-vm/kernel", "-initrd", "/fixtures/docker-vm/initramfs.gz",
		"-append", "console=ttyS0 quiet panic=-1")
	output := &guestCapture{}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	transcript := output.text.String()
	if err != nil || output.overflow || !strings.Contains(transcript, "AIPERMISSION_DOCKER_VM_PASS") ||
		!strings.Contains(transcript, "--- PASS: TestDockerGuestQualification") ||
		strings.Contains(transcript, "AIPERMISSION_DOCKER_VM_FAILED") || strings.Contains(transcript, "--- SKIP:") {
		t.Fatalf("owned Docker VM qualification: %v (overflow=%t)\n%s", err, output.overflow, transcript)
	}
}

type guestCapture struct {
	text     strings.Builder
	overflow bool
}

func (capture *guestCapture) Write(data []byte) (int, error) {
	const limit = 64 * 1024
	remaining := limit - capture.text.Len()
	if len(data) > remaining {
		capture.overflow = true
		capture.text.Write(data[:remaining])
	} else {
		capture.text.Write(data)
	}
	return len(data), nil
}
