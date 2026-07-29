package sessionenvprotocol

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

const (
	Version          = "APENV/1"
	maxPreludeBytes  = 256 * 1024
	bootstrapTimeout = 30 * time.Second
)

type Result struct {
	Prelude []byte
	Reader  *bufio.Reader
	Nonce   string
}

type Protocol struct {
	nonce      string
	generation int64
}

func New(generation int64) (*Protocol, error) {
	if generation < 1 {
		return nil, errors.New("session generation must be positive")
	}
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	return &Protocol{nonce: nonce, generation: generation}, nil
}

// Command returns a non-secret connector-owned wrapper command. Secret values
// are not embedded in this string and arrive only through framed stdin.
func (p *Protocol) Command() string {
	if p == nil {
		return ""
	}
	script := compactShellScript(bootstrapScript(p.nonce, p.generation))
	return "__apenv_user_shell=${SHELL:-/bin/sh}; export __apenv_user_shell; eval " + shellSingleQuote(script)
}

// Bootstrap applies the complete envelope to one POSIX interactive shell.
// The caller starts Command first. Metadata is acknowledged before any value
// frame is sent.
func (p *Protocol) Bootstrap(ctx context.Context, stdin io.Writer, stdout io.Reader, envelope *sessionenv.Envelope) (Result, error) {
	if p == nil || stdin == nil || stdout == nil || envelope == nil || envelope.Len() == 0 {
		return Result{}, errors.New("secret environment bootstrap requires a protocol, stdin, stdout, and a non-empty envelope")
	}
	bootstrapCtx, cancel := context.WithTimeout(ctx, bootstrapTimeout)
	defer cancel()
	reader := bufio.NewReader(stdout)
	frames, aggregateBytes, err := metadataFrames(envelope)
	if err != nil {
		return Result{}, err
	}
	if _, err := fmt.Fprintf(stdin, "%s %s %d %d %d\n", Version, p.nonce, p.generation, envelope.Len(), aggregateBytes); err != nil {
		return Result{}, fmt.Errorf("write environment header: %w", err)
	}
	if _, err := io.WriteString(stdin, frames+"META_END "+p.nonce+"\n"); err != nil {
		return Result{}, fmt.Errorf("write environment metadata: %w", err)
	}
	prelude, err := waitForFrame(
		bootstrapCtx,
		reader,
		"READY "+Version+" "+p.nonce+" "+strconv.FormatInt(p.generation, 10),
		p.Command(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("environment metadata was not accepted: %w", err)
	}
	if err := writeValueFrames(stdin, envelope, p.nonce); err != nil {
		return Result{}, err
	}
	afterMetadata, err := waitForFrame(bootstrapCtx, reader, "ACK "+Version+" "+p.nonce+" "+strconv.FormatInt(p.generation, 10))
	if err != nil {
		return Result{}, fmt.Errorf("environment bootstrap was not acknowledged: %w", err)
	}
	prelude = append(prelude, afterMetadata...)
	return Result{Prelude: prelude, Reader: reader, Nonce: p.nonce}, nil
}

func metadataFrames(envelope *sessionenv.Envelope) (string, int, error) {
	var builder strings.Builder
	total := 0
	err := envelope.WithEntries(func(entries []sessionenv.EntryView) error {
		for index, item := range entries {
			if err := sessionenv.ValidateName(item.Name); err != nil {
				return err
			}
			encodedLength := base64.StdEncoding.EncodedLen(len(item.Value))
			total += len(item.Value)
			replace := 0
			if item.ReplaceExisting {
				replace = 1
			}
			fmt.Fprintf(&builder, "META %d %s %d %d\n", index+1, item.Name, replace, encodedLength)
		}
		return nil
	})
	return builder.String(), total, err
}

func writeValueFrames(stdin io.Writer, envelope *sessionenv.Envelope, nonce string) error {
	return envelope.WithEntries(func(entries []sessionenv.EntryView) error {
		for index, item := range entries {
			encoded := base64.StdEncoding.EncodeToString(item.Value)
			if _, err := fmt.Fprintf(stdin, "VALUE %d %s\n", index+1, encoded); err != nil {
				return fmt.Errorf("write environment value frame: %w", err)
			}
		}
		if _, err := fmt.Fprintf(stdin, "END %s\n", nonce); err != nil {
			return fmt.Errorf("write environment end frame: %w", err)
		}
		return nil
	})
}

func waitForFrame(ctx context.Context, reader *bufio.Reader, expected string, ignoredSuffixes ...string) ([]byte, error) {
	type lineResult struct {
		line string
		err  error
	}
	prelude := bytes.Buffer{}
	for {
		result := make(chan lineResult, 1)
		go func() {
			line, err := reader.ReadString('\n')
			result <- lineResult{line: line, err: err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case item := <-result:
			normalized := strings.ReplaceAll(item.line, "\r", "")
			trimmed := strings.TrimSpace(normalized)
			ignored := false
			for _, suffix := range ignoredSuffixes {
				if suffix != "" && strings.HasSuffix(trimmed, suffix) {
					ignored = true
					break
				}
			}
			if ignored {
				continue
			}
			if strings.HasSuffix(trimmed, expected) {
				frameAt := strings.LastIndex(normalized, expected)
				if frameAt > 0 {
					if prelude.Len()+frameAt > maxPreludeBytes {
						return nil, errors.New("shell prelude exceeded safety limit")
					}
					prelude.WriteString(normalized[:frameAt])
				}
				return prelude.Bytes(), nil
			}
			if item.line != "" {
				if prelude.Len()+len(item.line) > maxPreludeBytes {
					return nil, errors.New("shell prelude exceeded safety limit")
				}
				prelude.WriteString(item.line)
			}
			if item.err != nil {
				return nil, item.err
			}
		}
	}
}

func randomNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate bootstrap nonce: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func compactShellScript(script string) string {
	lines := strings.Split(script, "\n")
	var builder strings.Builder
	previousOpenedBlock := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if builder.Len() > 0 {
			if previousOpenedBlock {
				builder.WriteByte(' ')
			} else {
				builder.WriteString("; ")
			}
		}
		builder.WriteString(line)
		previousOpenedBlock = strings.HasSuffix(line, " do") ||
			strings.HasSuffix(line, " then") ||
			strings.HasSuffix(line, " {")
	}
	return builder.String()
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func bootstrapScript(nonce string, generation int64) string {
	// Nonce and generation are locally generated non-secret values.
	return fmt.Sprintf(`
set +x +v
HISTFILE=/dev/null
export HISTFILE
__apenv_shell=${__apenv_user_shell:-/bin/sh}
unset __apenv_user_shell
case "$__apenv_shell" in /*) ;; *) __apenv_shell=/bin/sh ;; esac
[ -x "$__apenv_shell" ] || __apenv_shell=/bin/sh
stty -echo -icanon min 1 time 0 2>/dev/null || exit 90
__apenv_fail() { stty echo icanon 2>/dev/null || true; exit 97; }
IFS=' ' read -r __apenv_version __apenv_nonce __apenv_generation __apenv_count __apenv_total || __apenv_fail
[ "$__apenv_version" = '%s' ] || __apenv_fail
[ "$__apenv_nonce" = '%s' ] || __apenv_fail
[ "$__apenv_generation" = '%d' ] || __apenv_fail
case "$__apenv_count:$__apenv_total" in *[!0-9:]*|:|*: ) __apenv_fail ;; esac
[ "$__apenv_count" -le %d ] || __apenv_fail
[ "$__apenv_total" -le %d ] || __apenv_fail
__apenv_i=1
while [ "$__apenv_i" -le "$__apenv_count" ]; do
  IFS=' ' read -r __apenv_tag __apenv_index __apenv_name __apenv_replace __apenv_encoded_len || __apenv_fail
  [ "$__apenv_tag" = META ] || __apenv_fail
  [ "$__apenv_index" = "$__apenv_i" ] || __apenv_fail
  case "$__apenv_name" in ''|[!A-Z_]*|*[!A-Z0-9_]*) __apenv_fail ;; esac
  case "$__apenv_replace:$__apenv_encoded_len" in 0:[0-9]*|1:[0-9]*) ;; *) __apenv_fail ;; esac
  eval "[ \"\${$__apenv_name+x}\" = x ]" && [ "$__apenv_replace" != 1 ] && __apenv_fail
  eval "__apenv_name_$__apenv_i=\$__apenv_name"
  eval "__apenv_len_$__apenv_i=\$__apenv_encoded_len"
  __apenv_i=$((__apenv_i + 1))
done
IFS=' ' read -r __apenv_meta_end __apenv_meta_nonce || __apenv_fail
[ "$__apenv_meta_end" = META_END ] || __apenv_fail
[ "$__apenv_meta_nonce" = "$__apenv_nonce" ] || __apenv_fail
command -v base64 >/dev/null 2>&1 || __apenv_fail
printf '' | base64 -d >/dev/null 2>&1 || __apenv_fail
printf 'READY %s %%s %%s\n' "$__apenv_nonce" "$__apenv_generation"
__apenv_i=1
while [ "$__apenv_i" -le "$__apenv_count" ]; do
  IFS=' ' read -r __apenv_value_tag __apenv_value_index __apenv_encoded || __apenv_fail
  [ "$__apenv_value_tag" = VALUE ] || __apenv_fail
  [ "$__apenv_value_index" = "$__apenv_i" ] || __apenv_fail
  eval "__apenv_expected_len=\${__apenv_len_$__apenv_i}"
  [ "${#__apenv_encoded}" -eq "$__apenv_expected_len" ] || __apenv_fail
  __apenv_decoded=$(printf '%%s' "$__apenv_encoded" | base64 -d 2>/dev/null; __apenv_status=$?; printf .; exit "$__apenv_status") || __apenv_fail
  __apenv_decoded=${__apenv_decoded%%?}
  eval "__apenv_name=\${__apenv_name_$__apenv_i}"
  export "$__apenv_name=$__apenv_decoded" || __apenv_fail
  unset "__apenv_name_$__apenv_i" "__apenv_len_$__apenv_i"
  __apenv_i=$((__apenv_i + 1))
done
IFS=' ' read -r __apenv_end __apenv_end_nonce || __apenv_fail
[ "$__apenv_end" = END ] || __apenv_fail
[ "$__apenv_end_nonce" = "$__apenv_nonce" ] || __apenv_fail
unset __apenv_version __apenv_count __apenv_total __apenv_i __apenv_tag __apenv_index __apenv_name
unset __apenv_replace __apenv_encoded_len __apenv_meta_end __apenv_meta_nonce
unset __apenv_value_tag __apenv_value_index __apenv_encoded __apenv_expected_len __apenv_decoded __apenv_status __apenv_end __apenv_end_nonce
stty echo icanon 2>/dev/null || __apenv_fail
printf 'ACK %s %%s %%s\n' "$__apenv_nonce" "$__apenv_generation"
unset __apenv_nonce __apenv_generation
unset -f __apenv_fail 2>/dev/null || unset __apenv_fail
exec "$__apenv_shell" -i
`, Version, nonce, generation, sessionenv.MaxItems, sessionenv.MaxTotalBytes, Version, Version)
}
