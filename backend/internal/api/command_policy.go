package api

import (
	"regexp"
	"strings"
)

type commandPolicyWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type commandPolicyRule struct {
	Code    string
	Pattern *regexp.Regexp
	Message string
}

var commandPolicyRules = []commandPolicyRule{
	{
		Code:    "destructive_file_operation",
		Pattern: regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?(rm\s+(-[A-Za-z]*[rf][A-Za-z]*|--recursive|--force)|find\s+.+\s+-delete)\b`),
		Message: "This command may delete files. Inspect the target path and reason before running it.",
	},
	{
		Code:    "disk_or_partition_operation",
		Pattern: regexp.MustCompile(`(?i)\b(mkfs|fdisk|parted|wipefs|sfdisk|sgdisk|dd\s+if=|dd\s+of=)\b`),
		Message: "This command may modify disks or partitions.",
	},
	{
		Code:    "package_or_service_change",
		Pattern: regexp.MustCompile(`(?i)\b(apt(-get)?|dnf|yum|apk|pacman)\s+.*\b(install|remove|purge|upgrade|dist-upgrade|autoremove)\b|\bsystemctl\s+(restart|stop|disable|mask|enable)\b`),
		Message: "This command may change installed packages or service state.",
	},
	{
		Code:    "container_or_cluster_destructive_change",
		Pattern: regexp.MustCompile(`(?i)\b(docker|podman)\s+(rm|rmi|prune|compose\s+down|volume\s+rm)\b|\bkubectl\s+(delete|drain|cordon|uncordon|scale|apply|patch)\b`),
		Message: "This command may change container or cluster state.",
	},
	{
		Code:    "firewall_or_network_change",
		Pattern: regexp.MustCompile(`(?i)\b(ufw|iptables|nft|firewall-cmd|ip\s+route|ip\s+addr)\s+`),
		Message: "This command may change firewall or network configuration.",
	},
	{
		Code:    "credential_read",
		Pattern: regexp.MustCompile(`(?i)\b(cat|sed|awk|grep)\b.*(/etc/shadow|id_rsa|id_ed25519|\.pem|\.key|\.env)\b`),
		Message: "This command may print credentials or secret-bearing files. Prefer existence checks or masked output.",
	},
}

func analyzeCommandPolicy(command string) []commandPolicyWarning {
	text := strings.TrimSpace(command)
	if text == "" {
		return nil
	}
	warnings := []commandPolicyWarning{}
	for _, rule := range commandPolicyRules {
		if rule.Pattern.MatchString(text) {
			warnings = append(warnings, commandPolicyWarning{
				Code:     rule.Code,
				Severity: "warn",
				Message:  rule.Message,
			})
		}
	}
	return warnings
}
