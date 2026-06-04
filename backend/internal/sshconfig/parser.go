package sshconfig

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxConfigBytes = 256 * 1024

type HostEntry struct {
	Alias                  string   `json:"alias"`
	Host                   string   `json:"host"`
	Port                   int      `json:"port"`
	Username               string   `json:"username"`
	IdentityFile           string   `json:"identity_file,omitempty"`
	ProxyJump              string   `json:"proxy_jump,omitempty"`
	ProxyCommandConfigured bool     `json:"proxy_command_configured,omitempty"`
	Warnings               []string `json:"warnings,omitempty"`
}

func DiscoverDefault() ([]HostEntry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory: %w", err)
	}
	path := filepath.Join(home, ".ssh", "config")
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return []HostEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open ssh config: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read ssh config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("ssh config is larger than %d bytes", maxConfigBytes)
	}
	return Parse(string(data)), nil
}

func Parse(content string) []HostEntry {
	parser := configParser{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		key, value, ok := parseLine(scanner.Text())
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "host":
			parser.flush()
			parser.aliases = parseHostAliases(value)
		case "hostname":
			if parser.host == "" {
				parser.host = firstField(value)
			}
		case "user":
			if parser.username == "" {
				parser.username = firstField(value)
			}
		case "port":
			if parser.port == 0 {
				parser.port = parsePort(value)
			}
		case "identityfile":
			if parser.identityFile == "" {
				parser.identityFile = firstField(value)
			}
		case "proxyjump":
			if parser.proxyJump == "" {
				parser.proxyJump = firstField(value)
			}
		case "proxycommand":
			if !parser.proxyCommandConfigured {
				parser.proxyCommandConfigured = true
			}
		case "match":
			parser.flush()
			parser.reset()
		}
	}
	parser.flush()
	return parser.entries()
}

type configParser struct {
	aliases []string
	hostBlock
	blocks []hostBlock
}

type hostBlock struct {
	aliases                []string
	host                   string
	port                   int
	username               string
	identityFile           string
	proxyJump              string
	proxyCommandConfigured bool
}

func (p *configParser) flush() {
	if len(p.aliases) > 0 {
		block := p.hostBlock
		block.aliases = append([]string(nil), p.aliases...)
		p.blocks = append(p.blocks, block)
	}
	p.reset()
}

func (p *configParser) reset() {
	p.aliases = nil
	p.hostBlock = hostBlock{}
}

func (p *configParser) entries() []HostEntry {
	seen := map[string]bool{}
	var entries []HostEntry
	for _, block := range p.blocks {
		for _, alias := range block.aliases {
			if !isConcreteAlias(alias) || seen[alias] {
				continue
			}
			entry := HostEntry{Alias: alias}
			for _, candidate := range p.blocks {
				if candidate.matches(alias) {
					candidate.apply(&entry)
				}
			}
			if entry.Host == "" {
				entry.Host = alias
			}
			if entry.Port == 0 {
				entry.Port = 22
			}
			if strings.Contains(entry.Host, "%") || strings.Contains(entry.IdentityFile, "%") {
				entry.Warnings = append(entry.Warnings, "contains OpenSSH tokens that AIPermission will not expand automatically")
			}
			if entry.ProxyCommandConfigured {
				entry.Warnings = append(entry.Warnings, "ProxyCommand is configured but not imported")
			}
			entries = append(entries, entry)
			seen[alias] = true
		}
	}
	return entries
}

func (b hostBlock) matches(alias string) bool {
	for _, item := range b.aliases {
		if item == "*" || item == alias {
			return true
		}
	}
	return false
}

func (b hostBlock) apply(entry *HostEntry) {
	if entry.Host == "" && b.host != "" {
		entry.Host = b.host
	}
	if entry.Port == 0 && b.port != 0 {
		entry.Port = b.port
	}
	if entry.Username == "" && b.username != "" {
		entry.Username = b.username
	}
	if entry.IdentityFile == "" && b.identityFile != "" {
		entry.IdentityFile = b.identityFile
	}
	if entry.ProxyJump == "" && b.proxyJump != "" {
		entry.ProxyJump = b.proxyJump
	}
	if !entry.ProxyCommandConfigured && b.proxyCommandConfigured {
		entry.ProxyCommandConfigured = true
	}
}

func parseLine(line string) (string, string, bool) {
	line = stripComment(strings.TrimSpace(line))
	if line == "" {
		return "", "", false
	}
	if before, after, ok := strings.Cut(line, "="); ok && strings.TrimSpace(before) != "" {
		return strings.TrimSpace(before), strings.TrimSpace(after), true
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	key := fields[0]
	value := strings.TrimSpace(line[len(key):])
	if strings.HasPrefix(value, "=") {
		value = strings.TrimSpace(strings.TrimPrefix(value, "="))
	}
	return key, strings.TrimSpace(value), true
}

func stripComment(line string) string {
	var quoted byte
	for index := 0; index < len(line); index++ {
		char := line[index]
		if quoted != 0 {
			if char == quoted {
				quoted = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quoted = char
			continue
		}
		if char == '#' {
			return strings.TrimSpace(line[:index])
		}
	}
	return line
}

func parseHostAliases(value string) []string {
	aliases := []string{}
	for _, item := range strings.Fields(value) {
		item = strings.TrimSpace(item)
		if item != "" {
			aliases = append(aliases, item)
		}
	}
	return aliases
}

func isConcreteAlias(alias string) bool {
	return alias != "" && !strings.ContainsAny(alias, "*?!")
}

func hasConcreteAlias(aliases []string) bool {
	for _, alias := range aliases {
		if isConcreteAlias(alias) {
			return true
		}
	}
	return false
}

func firstField(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `"'`)
}

func parsePort(value string) int {
	port, err := strconv.Atoi(firstField(value))
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}
