package kafka

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func TestSCRAMRequiresExplicitTransportProtection(t *testing.T) {
	for _, mechanism := range []string{"scram_sha_256", "scram_sha_512"} {
		runtime := testRuntime([]string{"127.0.0.1:9092"}, &recordingDirectTransport{})
		runtime.Profile.Public = map[string]any{"mechanism": mechanism, "username": "reader"}
		runtime.Target.Config["allow_insecure_plain_sasl"] = true
		if _, err := parseClientConfig(t.Context(), runtime); err == nil || !strings.Contains(err.Error(), "requires TLS") {
			t.Fatalf("SCRAM must reject before secret access; PLAIN exception must not apply: %v", err)
		}
		runtime.Secrets = staticSecretAccessor{"password": "fixture-only"}
		runtime.Target.Config["allow_insecure_scram_sasl"] = true
		if _, err := parseClientConfig(t.Context(), runtime); err != nil {
			t.Fatalf("explicit SCRAM exception: %v", err)
		}
		runtime.Target.Config["allow_insecure_scram_sasl"] = false
		runtime.Target.Config["tls_enabled"] = true
		if _, err := parseClientConfig(t.Context(), runtime); err != nil {
			t.Fatalf("verified TLS SCRAM: %v", err)
		}
	}
}

func TestConnectionRejectsUnapprovedAdvertisedBroker(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1))
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	cluster.ControlKey(int16(kmsg.Metadata), func(request kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		response := request.ResponseKind().(*kmsg.MetadataResponse)
		response.Brokers = []kmsg.MetadataResponseBroker{{NodeID: 0, Host: "unapproved.invalid", Port: 9092}}
		return response, nil, true
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result, err := New().TestConnection(ctx, testRuntime(cluster.ListenAddrs(), &recordingDirectTransport{}))
	if err != nil || result.Status == connectors.TestOK || !strings.Contains(result.Message, "scope") {
		t.Fatalf("unapproved metadata reported reachable: %#v %v", result, err)
	}
}
