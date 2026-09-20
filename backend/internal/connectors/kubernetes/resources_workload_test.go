package kubernetesconnector

import "testing"

func TestWorkloadSummaryUsesKindSpecificReplicaCounts(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		spec      map[string]any
		status    map[string]any
		ready     string
		replicas  int
		available int
	}{
		{name: "deployment", kind: "Deployment", spec: map[string]any{"replicas": 4}, status: map[string]any{"readyReplicas": 3, "availableReplicas": 2}, ready: "3/4", replicas: 4, available: 2},
		{name: "stateful set", kind: "StatefulSet", spec: map[string]any{"replicas": 2}, status: map[string]any{"readyReplicas": 2, "availableReplicas": 2}, ready: "2/2", replicas: 2, available: 2},
		{name: "healthy daemon set", kind: "DaemonSet", status: map[string]any{"desiredNumberScheduled": 3, "numberReady": 3, "numberAvailable": 3}, ready: "3/3", replicas: 3, available: 3},
		{name: "partially unavailable daemon set", kind: "DaemonSet", status: map[string]any{"desiredNumberScheduled": 3, "numberReady": 2, "numberAvailable": 1}, ready: "2/3", replicas: 3, available: 1},
		{name: "zero desired daemon set", kind: "DaemonSet", status: map[string]any{"desiredNumberScheduled": 0, "numberReady": 0, "numberAvailable": 0}, ready: "0/0", replicas: 0, available: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := map[string]any{
				"kind":     test.kind,
				"metadata": map[string]any{"namespace": "default", "name": "fixture"},
				"spec":     test.spec,
				"status":   test.status,
			}
			summary := workloadSummaryFromItem(item)
			if summary.Ready != test.ready || summary.Replicas != test.replicas || summary.Available != test.available {
				t.Fatalf("summary=%#v, want ready=%s replicas=%d available=%d", summary, test.ready, test.replicas, test.available)
			}
		})
	}
}
