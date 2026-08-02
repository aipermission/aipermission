package mailconnector

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"golang.org/x/net/idna"
)

type targetConfig struct {
	ConnectionMode          string
	TransportTargetRef      string
	IMAPHost                string
	IMAPPort                int
	IMAPTLSMode             string
	SMTPHost                string
	SMTPPort                int
	SMTPTLSMode             string
	AllowedRecipientDomains []string
}

type profileConfig struct {
	MailboxAddress              string
	DisplayName                 string
	ReplyTo                     string
	IMAPEnabled                 bool
	SMTPAuthMode                string
	AllowedReadFolders          []string
	AllowedMutationSources      []string
	AllowedMutationDestinations []string
	SentFolder                  string
	ArchiveFolder               string
	TrashFolder                 string
}

func targetConfigFrom(target connectors.TargetView) (targetConfig, error) {
	imapPort, err := exactIntValue(target.Config, "imap_port", defaultIMAPPort)
	if err != nil {
		return targetConfig{}, fmt.Errorf("%w: imap_port must be an integer", ErrInvalidConfig)
	}
	smtpPort, err := exactIntValue(target.Config, "smtp_port", defaultSMTPPort)
	if err != nil {
		return targetConfig{}, fmt.Errorf("%w: smtp_port must be an integer", ErrInvalidConfig)
	}
	config := targetConfig{
		ConnectionMode:          stringValue(target.Config, "connection_mode"),
		TransportTargetRef:      strings.TrimSpace(stringValue(target.Config, "transport_target_ref")),
		IMAPHost:                strings.TrimSpace(stringValue(target.Config, "imap_host")),
		IMAPPort:                imapPort,
		IMAPTLSMode:             stringValue(target.Config, "imap_tls_mode"),
		SMTPHost:                strings.TrimSpace(stringValue(target.Config, "smtp_host")),
		SMTPPort:                smtpPort,
		SMTPTLSMode:             stringValue(target.Config, "smtp_tls_mode"),
		AllowedRecipientDomains: stringSlice(target.Config["allowed_recipient_domains"]),
	}
	if config.ConnectionMode == "" {
		config.ConnectionMode = "direct"
	}
	if config.IMAPTLSMode == "" {
		config.IMAPTLSMode = "implicit_tls"
	}
	if config.SMTPTLSMode == "" {
		config.SMTPTLSMode = "implicit_tls"
	}
	if config.ConnectionMode != "direct" && config.ConnectionMode != "over_ssh" {
		return targetConfig{}, fmt.Errorf("%w: unsupported connection_mode %q", ErrInvalidConfig, config.ConnectionMode)
	}
	if config.ConnectionMode == "over_ssh" && config.TransportTargetRef == "" {
		return targetConfig{}, fmt.Errorf("%w: transport_target_ref is required for over_ssh", ErrInvalidConfig)
	}
	if err := validateEndpoint("imap", config.IMAPHost, config.IMAPPort, config.IMAPTLSMode); err != nil {
		return targetConfig{}, err
	}
	config.IMAPHost = normalizeHost(config.IMAPHost)
	if err := validateEndpoint("smtp", config.SMTPHost, config.SMTPPort, config.SMTPTLSMode); err != nil {
		return targetConfig{}, err
	}
	config.SMTPHost = normalizeHost(config.SMTPHost)
	if len(config.AllowedRecipientDomains) > maxRecipientDomains {
		return targetConfig{}, fmt.Errorf("%w: allowed_recipient_domains exceeds %d entries", ErrInvalidConfig, maxRecipientDomains)
	}
	normalizedDomains := make([]string, 0, len(config.AllowedRecipientDomains))
	seenDomains := map[string]bool{}
	for index, domain := range config.AllowedRecipientDomains {
		normalized, err := normalizeDomain(domain)
		if err != nil {
			return targetConfig{}, fmt.Errorf("%w: allowed_recipient_domains[%d]: %v", ErrInvalidConfig, index, err)
		}
		if !seenDomains[normalized] {
			seenDomains[normalized] = true
			normalizedDomains = append(normalizedDomains, normalized)
		}
	}
	config.AllowedRecipientDomains = normalizedDomains
	return config, nil
}

func (Connector) ValidateTargetConfig(config map[string]any) error {
	_, err := targetConfigFrom(connectors.TargetView{Config: config})
	return err
}

func profileConfigFrom(profile connectors.CredentialProfileView) (profileConfig, error) {
	config := profileConfig{
		MailboxAddress:              strings.TrimSpace(stringValue(profile.Public, "mailbox_address")),
		DisplayName:                 strings.TrimSpace(stringValue(profile.Public, "display_name")),
		ReplyTo:                     strings.TrimSpace(stringValue(profile.Public, "reply_to")),
		IMAPEnabled:                 boolValue(profile.Public, "imap_enabled", true),
		SMTPAuthMode:                stringValue(profile.Public, "smtp_auth_mode"),
		AllowedReadFolders:          folderSlice(profile.Public["allowed_read_folders"], []string{defaultFolder}),
		AllowedMutationSources:      folderSlice(profile.Public["allowed_mutation_source_folders"], []string{defaultFolder}),
		AllowedMutationDestinations: folderSlice(profile.Public["allowed_mutation_destination_folders"], nil),
		SentFolder:                  strings.TrimSpace(stringValue(profile.Public, "sent_folder")),
		ArchiveFolder:               strings.TrimSpace(stringValue(profile.Public, "archive_folder")),
		TrashFolder:                 strings.TrimSpace(stringValue(profile.Public, "trash_folder")),
	}
	if config.SMTPAuthMode == "" {
		config.SMTPAuthMode = "disabled"
	}
	if config.SMTPAuthMode != "disabled" && config.SMTPAuthMode != "reuse_imap" && config.SMTPAuthMode != "separate" {
		return profileConfig{}, fmt.Errorf("%w: unsupported smtp_auth_mode %q", ErrInvalidConfig, config.SMTPAuthMode)
	}
	if !config.IMAPEnabled && config.SMTPAuthMode == "disabled" {
		return profileConfig{}, fmt.Errorf("%w: at least one of IMAP or SMTP must be enabled", ErrInvalidConfig)
	}
	if config.SMTPAuthMode == "reuse_imap" && !config.IMAPEnabled {
		return profileConfig{}, fmt.Errorf("%w: reuse_imap requires IMAP to be enabled", ErrInvalidConfig)
	}
	if _, err := parseMailboxAddress(config.MailboxAddress); err != nil {
		return profileConfig{}, fmt.Errorf("%w: mailbox_address: %v", ErrInvalidConfig, err)
	}
	if err := validateDisplayName(config.DisplayName); err != nil {
		return profileConfig{}, fmt.Errorf("%w: display_name: %v", ErrInvalidConfig, err)
	}
	if config.ReplyTo != "" {
		if _, err := parseMailboxAddress(config.ReplyTo); err != nil {
			return profileConfig{}, fmt.Errorf("%w: reply_to: %v", ErrInvalidConfig, err)
		}
	}
	if len(config.AllowedReadFolders) == 0 && config.IMAPEnabled {
		return profileConfig{}, fmt.Errorf("%w: at least one readable folder is required when IMAP is enabled", ErrInvalidConfig)
	}
	for label, folders := range map[string][]string{
		"allowed_read_folders":                 config.AllowedReadFolders,
		"allowed_mutation_source_folders":      config.AllowedMutationSources,
		"allowed_mutation_destination_folders": config.AllowedMutationDestinations,
	} {
		if len(folders) > maxPolicyFolders {
			return profileConfig{}, fmt.Errorf("%w: %s exceeds %d entries", ErrInvalidConfig, label, maxPolicyFolders)
		}
		for _, folder := range folders {
			if err := validateFolderName(folder); err != nil {
				return profileConfig{}, fmt.Errorf("%w: %s: %v", ErrInvalidConfig, label, err)
			}
		}
	}
	if config.SentFolder != "" {
		if err := validateFolderName(config.SentFolder); err != nil {
			return profileConfig{}, fmt.Errorf("%w: sent_folder: %v", ErrInvalidConfig, err)
		}
	}
	if config.ArchiveFolder != "" && !folderAllowed(config.ArchiveFolder, config.AllowedMutationDestinations) {
		return profileConfig{}, fmt.Errorf("%w: archive_folder must be an allowed mutation destination", ErrInvalidConfig)
	}
	if config.TrashFolder != "" && !folderAllowed(config.TrashFolder, config.AllowedMutationDestinations) {
		return profileConfig{}, fmt.Errorf("%w: trash_folder must be an allowed mutation destination", ErrInvalidConfig)
	}
	return config, nil
}

func validateDisplayName(value string) error {
	if len(value) > maxDisplayNameBytes {
		return fmt.Errorf("must not exceed %d bytes", maxDisplayNameBytes)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("must not contain control characters")
		}
	}
	return nil
}

func (Connector) ValidateCredentialProfile(kind string, public, secret map[string]any, _ *connectors.CredentialProfileView) error {
	if kind != "password" {
		return fmt.Errorf("unsupported mail credential kind %q", kind)
	}
	profile := connectors.CredentialProfileView{Kind: kind, Public: public}
	config, err := profileConfigFrom(profile)
	if err != nil {
		return err
	}
	if config.IMAPEnabled {
		if strings.TrimSpace(stringValue(secret, "imap_username")) == "" || strings.TrimSpace(stringValue(secret, "imap_password")) == "" {
			return fmt.Errorf("imap_username and imap_password are required while IMAP is enabled")
		}
	}
	if config.SMTPAuthMode == "separate" {
		if strings.TrimSpace(stringValue(secret, "smtp_username")) == "" || strings.TrimSpace(stringValue(secret, "smtp_password")) == "" {
			return fmt.Errorf("smtp_username and smtp_password are required for separate SMTP authentication")
		}
	}
	return nil
}

func validateEndpoint(protocol, host string, port int, tlsMode string) error {
	if err := validateHost(host); err != nil {
		return fmt.Errorf("%w: %s_host: %v", ErrInvalidConfig, protocol, err)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: %s_port must be between 1 and 65535", ErrInvalidConfig, protocol)
	}
	if tlsMode != "implicit_tls" && tlsMode != "starttls" {
		return fmt.Errorf("%w: unsupported %s_tls_mode %q", ErrInvalidConfig, protocol, tlsMode)
	}
	return nil
}

func validateHost(host string) error {
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if strings.ContainsAny(host, "\r\n\t/\\") || strings.Contains(host, "://") || strings.Contains(host, "@") {
		return fmt.Errorf("host must not include a scheme, path, credentials, or control characters")
	}
	for _, character := range host {
		if unicode.IsControl(character) {
			return fmt.Errorf("host must not include control characters")
		}
	}
	if parsed := net.ParseIP(strings.Trim(host, "[]")); parsed != nil {
		return nil
	}
	if strings.Contains(host, ":") {
		return fmt.Errorf("host must not include a port")
	}
	ascii, err := idna.Lookup.ToASCII(strings.TrimSuffix(host, "."))
	if err != nil || ascii == "" || len(ascii) > 253 {
		return fmt.Errorf("host is invalid")
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("host is invalid")
		}
	}
	return nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.String()
	}
	ascii, err := idna.Lookup.ToASCII(strings.TrimSuffix(host, "."))
	if err != nil {
		return strings.TrimSuffix(strings.ToLower(host), ".")
	}
	return strings.ToLower(ascii)
}

func tlsConfigFor(host string) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: strings.Trim(host, "[]")}
}

func parseMailboxAddress(value string) (*mail.Address, error) {
	if strings.ContainsAny(value, "\r\n") || len(value) > maxAddressBytes {
		return nil, fmt.Errorf("address is invalid or too long")
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address == "" {
		return nil, fmt.Errorf("address is invalid")
	}
	local, domain, ok := strings.Cut(address.Address, "@")
	if !ok || local == "" || domain == "" || !isASCII(local) {
		return nil, fmt.Errorf("address must contain an ASCII local part and a domain")
	}
	normalizedDomain, err := normalizeDomain(domain)
	if err != nil {
		return nil, err
	}
	address.Address = local + "@" + normalizedDomain
	return address, nil
}

func normalizeDomain(value string) (string, error) {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if value == "" || strings.ContainsAny(value, "\r\n/@:") {
		return "", fmt.Errorf("domain is invalid")
	}
	normalized, err := idna.Lookup.ToASCII(value)
	if err != nil || normalized == "" || len(normalized) > 253 {
		return "", fmt.Errorf("domain is invalid")
	}
	return strings.ToLower(normalized), nil
}

func isASCII(value string) bool {
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError || r > 127 {
			return false
		}
		value = value[size:]
	}
	return true
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func rawStringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return fmt.Sprint(value)
	}
	return text
}

func exactIntValue(values map[string]any, key string, fallback int) (int, error) {
	text := stringValue(values, key)
	if text == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(text, 10, strconv.IntSize)
	if err != nil {
		return 0, fmt.Errorf("value is not an integer")
	}
	return int(parsed), nil
}

func boolValue(values map[string]any, key string, fallback bool) bool {
	if values == nil || values[key] == nil {
		return fallback
	}
	value, ok := values[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func stringSlice(value any) []string {
	var raw []string
	switch typed := value.(type) {
	case []string:
		raw = typed
	case []any:
		for _, item := range typed {
			raw = append(raw, fmt.Sprint(item))
		}
	case string:
		for _, item := range strings.FieldsFunc(typed, func(r rune) bool { return r == ',' || r == '\n' }) {
			raw = append(raw, item)
		}
	}
	result := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

func folderSlice(value any, fallback []string) []string {
	items := stringSlice(value)
	if len(items) == 0 && value == nil {
		return append([]string(nil), fallback...)
	}
	return items
}

func folderAllowed(folder string, allowed []string) bool {
	for _, candidate := range allowed {
		if folderEqual(candidate, folder) {
			return true
		}
	}
	return false
}

func folderEqual(left, right string) bool {
	return left == right || strings.EqualFold(left, defaultFolder) && strings.EqualFold(right, defaultFolder)
}

func requireFolder(folder string, allowed []string) (string, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		folder = defaultFolder
	}
	if err := validateFolderName(folder); err != nil {
		return "", fmt.Errorf("%w: %v", ErrFolderDenied, err)
	}
	if !folderAllowed(folder, allowed) {
		return "", fmt.Errorf("%w: %q", ErrFolderDenied, folder)
	}
	return folder, nil
}

func requireExplicitFolder(folder string, allowed []string) (string, error) {
	if strings.TrimSpace(folder) == "" {
		return "", fmt.Errorf("%w: destination folder is required", ErrFolderDenied)
	}
	return requireFolder(folder, allowed)
}

func validateFolderName(folder string) error {
	if folder == "" || len(folder) > maxFolderNameBytes {
		return fmt.Errorf("folder name is empty or exceeds %d bytes", maxFolderNameBytes)
	}
	for _, value := range folder {
		if value == 0 || value == '\r' || value == '\n' || value < 32 || value == 127 {
			return fmt.Errorf("folder name contains control characters")
		}
	}
	return nil
}

func recipientDomainAllowed(address *mail.Address, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	_, domain, _ := strings.Cut(address.Address, "@")
	for _, candidate := range allowed {
		if domain == candidate || strings.HasSuffix(domain, "."+candidate) {
			return true
		}
	}
	return false
}

func endpointURL(host string, port int) string {
	return (&url.URL{Scheme: "tcp", Host: net.JoinHostPort(host, strconv.Itoa(port))}).String()
}
