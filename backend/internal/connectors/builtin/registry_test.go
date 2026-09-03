package builtin

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	clickhouseconnector "github.com/aipermission/aipermission/backend/internal/connectors/clickhouse"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	kafkaconnector "github.com/aipermission/aipermission/backend/internal/connectors/kafka"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	mailconnector "github.com/aipermission/aipermission/backend/internal/connectors/mail"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	rabbitmqconnector "github.com/aipermission/aipermission/backend/internal/connectors/rabbitmq"
	redisconnector "github.com/aipermission/aipermission/backend/internal/connectors/redis"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

func TestNewRegistryIncludesBuiltInConnectors(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	clickhouse, ok := registry.Get(clickhouseconnector.Kind)
	if !ok {
		t.Fatal("expected clickhouse connector")
	}
	if clickhouse.Label() != clickhouseconnector.Label {
		t.Fatalf("clickhouse label = %q", clickhouse.Label())
	}

	docker, ok := registry.Get(dockerconnector.Kind)
	if !ok {
		t.Fatal("expected docker connector")
	}
	if docker.Label() != dockerconnector.Label {
		t.Fatalf("docker label = %q", docker.Label())
	}
	kafka, ok := registry.Get(kafkaconnector.Kind)
	if !ok {
		t.Fatal("expected kafka connector")
	}
	if kafka.Label() != kafkaconnector.Label {
		t.Fatalf("kafka label = %q", kafka.Label())
	}
	kubernetes, ok := registry.Get(kubernetesconnector.Kind)
	if !ok {
		t.Fatal("expected kubernetes connector")
	}
	mail, ok := registry.Get(mailconnector.Kind)
	if !ok {
		t.Fatal("expected mail connector")
	}
	if mail.Label() != mailconnector.Label {
		t.Fatalf("mail label = %q", mail.Label())
	}
	if kubernetes.Label() != kubernetesconnector.Label {
		t.Fatalf("kubernetes label = %q", kubernetes.Label())
	}

	postgres, ok := registry.Get(postgresconnector.Kind)
	if !ok {
		t.Fatal("expected postgres connector")
	}
	if postgres.Label() != postgresconnector.Label {
		t.Fatalf("postgres label = %q", postgres.Label())
	}
	rabbitmq, ok := registry.Get(rabbitmqconnector.Kind)
	if !ok {
		t.Fatal("expected rabbitmq connector")
	}
	if rabbitmq.Label() != rabbitmqconnector.Label {
		t.Fatalf("rabbitmq label = %q", rabbitmq.Label())
	}
	redis, ok := registry.Get(redisconnector.Kind)
	if !ok {
		t.Fatal("expected redis connector")
	}
	if redis.Label() != redisconnector.Label {
		t.Fatalf("redis label = %q", redis.Label())
	}
	s3, ok := registry.Get(s3connector.Kind)
	if !ok {
		t.Fatal("expected s3 connector")
	}
	if s3.Label() != s3connector.Label {
		t.Fatalf("s3 label = %q", s3.Label())
	}

	connector, ok := registry.Get(sshconnector.Kind)
	if !ok {
		t.Fatal("expected ssh connector")
	}
	if connector.Label() != sshconnector.Label {
		t.Fatalf("label = %q", connector.Label())
	}

	infos := registry.List()
	if len(infos) != 10 || infos[0].Kind != clickhouseconnector.Kind || infos[1].Kind != dockerconnector.Kind || infos[2].Kind != kafkaconnector.Kind || infos[3].Kind != kubernetesconnector.Kind || infos[4].Kind != mailconnector.Kind || infos[5].Kind != postgresconnector.Kind || infos[6].Kind != rabbitmqconnector.Kind || infos[7].Kind != redisconnector.Kind || infos[8].Kind != s3connector.Kind || infos[9].Kind != sshconnector.Kind {
		t.Fatalf("unexpected connector list: %#v", infos)
	}
}

func TestRegisterAllPropagatesRegistryErrors(t *testing.T) {
	registry := connectors.NewRegistry()
	if err := registry.Register(sshconnector.New()); err != nil {
		t.Fatalf("seed ssh: %v", err)
	}

	if err := RegisterAll(registry); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestBuiltInConnectorPrepareActionsAreDeterministic(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	for _, info := range registry.List() {
		connector, ok := registry.Get(info.Kind)
		if !ok {
			t.Fatalf("connector %q missing from registry", info.Kind)
		}
		target, profile, inputs := builtInDeterminismSamples(t, info.Kind)
		actions, err := connector.GetActionList(context.Background(), target, profile)
		if err != nil {
			t.Fatalf("%s action list: %v", info.Kind, err)
		}
		if len(actions) != len(inputs) {
			t.Fatalf("%s sample inputs do not cover all actions: actions=%d samples=%d", info.Kind, len(actions), len(inputs))
		}
		for _, action := range actions {
			input, ok := inputs[action.Name]
			if !ok {
				t.Fatalf("%s missing deterministic sample for action %q", info.Kind, action.Name)
			}
			connectortest.AssertPrepareActionDeterministic(t, connector, connectors.ActionRequest{
				Target:     target,
				Profile:    profile,
				ActionName: action.Name,
				Input:      input,
				Reason:     "contract smoke",
			})
		}
	}
}

func builtInDeterminismSamples(t *testing.T, kind string) (connectors.TargetView, connectors.CredentialProfileView, map[string]map[string]any) {
	t.Helper()

	switch kind {
	case clickhouseconnector.Kind:
		return connectors.TargetView{
				ID:            8,
				Ref:           "clickhouse:8:80",
				ConnectorKind: clickhouseconnector.Kind,
				Name:          "analytics",
				Config:        map[string]any{"connection_mode": "direct", "host": "127.0.0.1", "port": 9000, "database": "default", "tls_mode": "disable"},
			}, connectors.CredentialProfileView{
				ID:            80,
				TargetID:      8,
				ConnectorKind: clickhouseconnector.Kind,
				Kind:          "username_password",
				Label:         "readonly",
				Public:        map[string]any{"username": "reader"},
			}, map[string]map[string]any{
				clickhouseconnector.ActionGetDatabases:  {},
				clickhouseconnector.ActionGetTables:     {"database": "analytics"},
				clickhouseconnector.ActionDescribeTable: {"database": "analytics", "table": "events"},
				clickhouseconnector.ActionQueryReadonly: {"sql": "select 1", "max_rows": 1},
			}
	case dockerconnector.Kind:
		return connectors.TargetView{
				ID:            5,
				Ref:           "docker:5:50",
				ConnectorKind: dockerconnector.Kind,
				Name:          "docker-host",
				Config:        map[string]any{"connection_mode": "over_ssh", "transport_target_ref": "ssh:2:20", "docker_command": "docker"},
			}, connectors.CredentialProfileView{
				ID:            50,
				TargetID:      5,
				ConnectorKind: dockerconnector.Kind,
				Kind:          "container_scope",
				Label:         "api-only",
				Public:        map[string]any{"scope_mode": "selected", "allowed_containers": "api"},
			}, map[string]map[string]any{
				dockerconnector.ActionVersion:          {},
				dockerconnector.ActionListContainers:   {"all": true},
				dockerconnector.ActionListImages:       {},
				dockerconnector.ActionListNetworks:     {},
				dockerconnector.ActionListVolumes:      {},
				dockerconnector.ActionInspectContainer: {"container": "api"},
				dockerconnector.ActionContainerLogs:    {"container": "api", "tail": 100},
				dockerconnector.ActionContainerExec:    {"container": "api", "command": "printf ok"},
				dockerconnector.ActionStartContainer:   {"container": "api"},
				dockerconnector.ActionStopContainer:    {"container": "api", "timeout_seconds": 10},
				dockerconnector.ActionRestartContainer: {"container": "api", "timeout_seconds": 10},
			}
	case kubernetesconnector.Kind:
		return connectors.TargetView{
				ID:            6,
				Ref:           "kubernetes:6:60",
				ConnectorKind: kubernetesconnector.Kind,
				Name:          "cluster",
				Config:        map[string]any{"connection_mode": "over_ssh", "transport_target_ref": "ssh:2:20", "kubectl_command": "kubectl"},
			}, connectors.CredentialProfileView{
				ID:            60,
				TargetID:      6,
				ConnectorKind: kubernetesconnector.Kind,
				Kind:          "namespace_scope",
				Label:         "production",
				Public:        map[string]any{"scope_mode": "selected", "namespaces": "production"},
			}, map[string]map[string]any{
				kubernetesconnector.ActionVersion:        {},
				kubernetesconnector.ActionListNamespaces: {},
				kubernetesconnector.ActionListWorkloads:  {"namespace": "production"},
				kubernetesconnector.ActionListPods:       {"namespace": "production"},
				kubernetesconnector.ActionListServices:   {"namespace": "production"},
				kubernetesconnector.ActionListIngress:    {"namespace": "production"},
				kubernetesconnector.ActionListNodes:      {},
				kubernetesconnector.ActionListEvents:     {"namespace": "production", "limit": 10},
				kubernetesconnector.ActionDescribe:       {"resource_type": "deployment", "namespace": "production", "name": "api"},
				kubernetesconnector.ActionLogs:           {"namespace": "production", "pod": "api-123", "container": "api", "tail": 100},
				kubernetesconnector.ActionRolloutRestart: {"namespace": "production", "deployment": "api"},
			}
	case kafkaconnector.Kind:
		return connectors.TargetView{
				ID:            9,
				Ref:           "kafka:9:90",
				ConnectorKind: kafkaconnector.Kind,
				Name:          "events",
				Config:        map[string]any{"server_family": "redpanda", "connection_mode": "direct", "bootstrap_brokers": "127.0.0.1:9092", "tls_enabled": false},
			}, connectors.CredentialProfileView{
				ID:            90,
				TargetID:      9,
				ConnectorKind: kafkaconnector.Kind,
				Kind:          "sasl",
				Label:         "monitor",
				Public:        map[string]any{"mechanism": "none"},
			}, map[string]map[string]any{
				kafkaconnector.ActionClusterInfo:            {},
				kafkaconnector.ActionListTopics:             {"include_internal": false},
				kafkaconnector.ActionDescribeTopic:          {"topic": "events"},
				kafkaconnector.ActionListConsumerGroups:     {},
				kafkaconnector.ActionDescribeConsumerGroup:  {"group": "workers"},
				kafkaconnector.ActionReadMessages:           {"topic": "events", "partition": 0, "start_position": "recent", "offset": "0", "max_records": 20, "max_bytes": 262144, "wait_seconds": 2},
				kafkaconnector.ActionPublishMessage:         {"topic": "events", "partition": 0, "key": "", "key_encoding": "utf8", "value": "test", "value_encoding": "utf8", "headers": []any{}},
				kafkaconnector.ActionSetConsumerGroupOffset: {"group": "workers", "topic": "events", "partition": 0, "offset": "0"},
			}
	case mailconnector.Kind:
		messageRef := map[string]any{"folder": "INBOX", "uidvalidity": 42, "uid": 7}
		return connectors.TargetView{
				ID:            10,
				Ref:           "mail:10:100",
				ConnectorKind: mailconnector.Kind,
				Name:          "support-mailbox",
				Config: map[string]any{
					"connection_mode": "direct", "imap_host": "imap.example.com", "imap_port": 993, "imap_tls_mode": "implicit_tls",
					"smtp_host": "smtp.example.com", "smtp_port": 465, "smtp_tls_mode": "implicit_tls", "allowed_recipient_domains": []any{"example.com"},
				},
			}, connectors.CredentialProfileView{
				ID:            100,
				TargetID:      10,
				ConnectorKind: mailconnector.Kind,
				Kind:          "password",
				Label:         "support",
				Public: map[string]any{
					"mailbox_address": "support@example.com", "imap_enabled": true, "smtp_auth_mode": "reuse_imap",
					"allowed_read_folders": []any{"INBOX", "Archive", "Trash"}, "allowed_mutation_source_folders": []any{"INBOX"},
					"allowed_mutation_destination_folders": []any{"Archive", "Trash"}, "archive_folder": "Archive", "trash_folder": "Trash",
				},
			}, map[string]map[string]any{
				mailconnector.ActionListFolders:     {},
				mailconnector.ActionCheckMailbox:    {"folder": "INBOX", "limit": 20},
				mailconnector.ActionSearchMessages:  {"folder": "INBOX", "unread_only": true, "subject": "status", "limit": 20},
				mailconnector.ActionGetMessage:      {"message_ref": messageRef},
				mailconnector.ActionListAttachments: {"message_ref": messageRef},
				mailconnector.ActionMarkRead:        {"message_ref": messageRef},
				mailconnector.ActionMarkUnread:      {"message_ref": messageRef},
				mailconnector.ActionMoveMessage:     {"message_ref": messageRef, "destination_folder": "Archive"},
				mailconnector.ActionArchiveMessage:  {"message_ref": messageRef},
				mailconnector.ActionSendMessage:     {"to": []any{"operator@example.com"}, "subject": "Status", "text_body": "All systems operational."},
				mailconnector.ActionReplyMessage:    {"message_ref": messageRef, "to": []any{"operator@example.com"}, "subject": "Re: Status", "text_body": "Acknowledged."},
				mailconnector.ActionDeleteMessage:   {"message_ref": messageRef},
			}
	case postgresconnector.Kind:
		return connectors.TargetView{
				ID:            1,
				Ref:           "postgres:1:10",
				ConnectorKind: postgresconnector.Kind,
				Name:          "main-db",
				Config:        map[string]any{"database": "appdb"},
			}, connectors.CredentialProfileView{
				ID:            10,
				TargetID:      1,
				ConnectorKind: postgresconnector.Kind,
				Kind:          "username_password",
				Label:         "readonly",
			}, map[string]map[string]any{
				postgresconnector.ActionGetSchemas:    {},
				postgresconnector.ActionGetTables:     {"schema": "public", "include_system": false},
				postgresconnector.ActionDescribeTable: {"schema": "public", "table": "users"},
				postgresconnector.ActionQueryReadonly: {"sql": "select 1", "max_rows": 1},
			}
	case redisconnector.Kind:
		return connectors.TargetView{
				ID:            3,
				Ref:           "redis:3:30",
				ConnectorKind: redisconnector.Kind,
				Name:          "cache",
				Config:        map[string]any{"connection_mode": "direct", "host": "127.0.0.1", "port": 6379, "database": 0},
			}, connectors.CredentialProfileView{
				ID:            30,
				TargetID:      3,
				ConnectorKind: redisconnector.Kind,
				Kind:          "username_password",
				Label:         "default",
			}, map[string]map[string]any{
				redisconnector.ActionPing:       {},
				redisconnector.ActionInfo:       {"section": "server"},
				redisconnector.ActionScanKeys:   {"pattern": "*", "limit": 10},
				redisconnector.ActionGetKey:     {"key": "app:test"},
				redisconnector.ActionSetString:  {"key": "app:test", "value": "hello", "ttl_seconds": 60},
				redisconnector.ActionExpireKey:  {"key": "app:test", "ttl_seconds": 60},
				redisconnector.ActionDeleteKeys: {"keys": []any{"app:test"}},
			}
	case rabbitmqconnector.Kind:
		return connectors.TargetView{
				ID:            4,
				Ref:           "rabbitmq:4:40",
				ConnectorKind: rabbitmqconnector.Kind,
				Name:          "queue",
				Config:        map[string]any{"connection_mode": "direct", "scheme": "http", "host": "127.0.0.1", "port": 15672, "vhost": "/"},
			}, connectors.CredentialProfileView{
				ID:            40,
				TargetID:      4,
				ConnectorKind: rabbitmqconnector.Kind,
				Kind:          "username_password",
				Label:         "monitor",
				Public:        map[string]any{"username": "guest"},
			}, map[string]map[string]any{
				rabbitmqconnector.ActionOverview:     {},
				rabbitmqconnector.ActionListVhosts:   {},
				rabbitmqconnector.ActionListQueues:   {"vhost": "/", "pattern": "", "limit": 10},
				rabbitmqconnector.ActionGetQueue:     {"vhost": "/", "queue": "jobs"},
				rabbitmqconnector.ActionListBindings: {"vhost": "/", "queue": "jobs", "limit": 10},
				rabbitmqconnector.ActionPeekMessages: {"vhost": "/", "queue": "jobs", "count": 2, "max_payload_bytes": 4096},
				rabbitmqconnector.ActionPublish:      {"vhost": "/", "exchange": "amq.default", "routing_key": "jobs", "payload": `{"ok":true}`, "payload_encoding": "string", "properties": map[string]any{"content_type": "application/json"}},
			}
	case s3connector.Kind:
		return connectors.TargetView{
				ID:            7,
				Ref:           "s3:7:70",
				ConnectorKind: s3connector.Kind,
				Name:          "object-store",
				Config:        map[string]any{"connection_mode": "direct", "scheme": "https", "host": "s3.example.com", "port": 443, "region": "us-east-1", "bucket": "app-backups", "path_style": true},
			}, connectors.CredentialProfileView{
				ID:            70,
				TargetID:      7,
				ConnectorKind: s3connector.Kind,
				Kind:          "access_key",
				Label:         "backup",
				Public:        map[string]any{"access_key_id": "AKIAEXAMPLE"},
			}, map[string]map[string]any{
				s3connector.ActionBucketInfo:        {},
				s3connector.ActionListObjects:       {"prefix": "daily/", "search": "db", "limit": 10},
				s3connector.ActionGetObjectMetadata: {"key": "daily/app.aipdb"},
				s3connector.ActionDownloadObject:    {"key": "daily/app.aipdb", "max_bytes": 1024},
				s3connector.ActionUploadObject:      {"key": "daily/app.txt", "content_text": "hello", "content_type": "text/plain", "overwrite": true},
				s3connector.ActionDeleteObject:      {"key": "daily/app-renamed.txt"},
				s3connector.ActionPresignDownload:   {"key": "daily/app.aipdb", "expires_seconds": 900},
				s3connector.ActionPresignUpload:     {"key": "incoming/app.aipdb", "expires_seconds": 900, "overwrite": false},
				s3connector.ActionListVersions:      {"key": "daily/app.aipdb", "limit": 10},
				s3connector.ActionRestoreVersion:    {"key": "daily/app.aipdb", "version_id": "version-1", "expected_current_etag": `"current-etag"`},
				s3connector.ActionDeleteVersion:     {"key": "daily/app.aipdb", "version_id": "version-2"},
				s3connector.ActionGetLifecycle:      {},
				s3connector.ActionReplaceLifecycle:  {"rule_id": "cleanup", "prefix": "tmp/", "expire_current_after_days": 30, "abort_incomplete_multipart_days": 7, "enabled": true},
				s3connector.ActionDeleteLifecycle:   {},
			}
	case sshconnector.Kind:
		return connectors.TargetView{
				ID:            2,
				Ref:           "ssh:2:20",
				ConnectorKind: sshconnector.Kind,
				Name:          "worker",
			}, connectors.CredentialProfileView{
				ID:            20,
				TargetID:      2,
				ConnectorKind: sshconnector.Kind,
				Kind:          "private_key",
				Label:         "root",
			}, map[string]map[string]any{
				sshconnector.ActionExec:                  {"command": "echo aipermission"},
				sshconnector.ActionReadConsole:           {"tail_bytes": 2048},
				sshconnector.ActionRestartConsoleSession: {},
				sshconnector.ActionBrowseRemoteFiles:     {"path": "~"},
				sshconnector.ActionStartFileDownload:     {"remote_paths": []any{"/etc/hosts"}, "archive_name": "hosts.zip"},
			}
	default:
		t.Fatalf("missing built-in deterministic samples for connector %q", kind)
	}
	return connectors.TargetView{}, connectors.CredentialProfileView{}, nil
}
