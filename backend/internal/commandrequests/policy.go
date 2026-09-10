package commandrequests

import (
	"regexp"
	"strings"
)

type PolicyWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type policyRule struct {
	code    string
	pattern *regexp.Regexp
	message string
}

var policyRules = []policyRule{
	{
		code:    "destructive_file_operation",
		pattern: regexp.MustCompile(`(?i)(^|[;&|]\s*)(sudo\s+)?(rm\s+(-[A-Za-z]*[rf][A-Za-z]*|--recursive|--force)|find\s+.+\s+-delete)\b`),
		message: "This command may delete files. Inspect the target path and reason before running it.",
	},
	{
		code:    "disk_or_partition_operation",
		pattern: regexp.MustCompile(`(?i)\b(mkfs|fdisk|parted|wipefs|sfdisk|sgdisk|dd\s+if=|dd\s+of=)\b`),
		message: "This command may modify disks or partitions.",
	},
	{
		code:    "package_or_service_change",
		pattern: regexp.MustCompile(`(?i)\b(apt(-get)?|dnf|yum|apk|pacman)\s+.*\b(install|remove|purge|upgrade|dist-upgrade|autoremove)\b|\bsystemctl\s+(restart|stop|disable|mask|enable)\b`),
		message: "This command may change installed packages or service state.",
	},
	{
		code:    "container_or_cluster_destructive_change",
		pattern: regexp.MustCompile(`(?i)\b(docker|podman)\s+(rm|rmi|prune|compose\s+down|volume\s+rm)\b|\bkubectl\s+(delete|drain|cordon|uncordon|scale|apply|patch)\b`),
		message: "This command may change container or cluster state.",
	},
	{
		code:    "firewall_or_network_change",
		pattern: regexp.MustCompile(`(?i)\b(ufw|iptables|nft|firewall-cmd|ip\s+route|ip\s+addr)\s+`),
		message: "This command may change firewall or network configuration.",
	},
	{
		code:    "credential_read",
		pattern: regexp.MustCompile(`(?i)\b(cat|sed|awk|grep)\b.*(/etc/shadow|id_rsa|id_ed25519|\.pem|\.key|\.env)\b`),
		message: "This command may print credentials or secret-bearing files. Prefer existence checks or masked output.",
	},
}

func AnalyzePolicy(command string) []PolicyWarning {
	text := strings.TrimSpace(command)
	if text == "" {
		return nil
	}
	warnings := make([]PolicyWarning, 0)
	for _, rule := range policyRules {
		if rule.pattern.MatchString(text) {
			warnings = append(warnings, PolicyWarning{
				Code: rule.code, Severity: "warn", Message: rule.message,
			})
		}
	}
	return warnings
}
