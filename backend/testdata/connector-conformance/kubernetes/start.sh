#!/bin/sh
set -eu
umask 077
mkdir -p /run/kube /kube-material
touch /kube-material/audit.jsonl
chmod 600 /kube-material/audit.jsonl
node_ip=$(hostname -i)
case "$node_ip" in ""|*[!0-9.]*) echo "owned fixture requires one IPv4 address" >&2; exit 1 ;; esac
k3s server --disable-agent --disable-scheduler --disable-cloud-controller \
  --disable-helm-controller --disable coredns,traefik,servicelb,local-storage,metrics-server \
  --egress-selector-mode disabled --kube-controller-manager-arg=controllers=-deployment,-replicaset \
  --kube-apiserver-arg=audit-policy-file=/fixture/audit.yaml \
  --kube-apiserver-arg=audit-log-path=/kube-material/audit.jsonl \
  --kube-apiserver-arg=audit-log-maxsize=1 --kube-apiserver-arg=audit-log-maxbackup=1 \
  --node-ip "$node_ip" --advertise-address "$node_ip" --tls-san kube-api --data-dir /run/kube/data \
  --write-kubeconfig /run/kube/admin.yaml --write-kubeconfig-mode 600 &
server=$!
cleanup() {
  trap - EXIT INT TERM
  kill "$server" 2>/dev/null || true
  wait "$server" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
kube() { /bin/kubectl --kubeconfig /run/kube/admin.yaml --request-timeout=5s "$@"; }
ready=0
for attempt in $(seq 1 90); do
  kill -0 "$server"
  if kube get --raw /readyz >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
test "$ready" = 1
kube apply -f /fixture/scope.yaml
kube config view --raw -o 'jsonpath={.clusters[0].cluster.certificate-authority-data}' \
  | base64 -d > /kube-material/ca.crt
kube create token scoped -n fixture-allowed --duration=20m > /kube-material/scoped-token
kube create token observer -n fixture-allowed --duration=20m > /kube-material/observer-token
config=/kube-material/scoped.yaml
/bin/kubectl config --kubeconfig "$config" set-cluster fixture \
  --server=https://kube-api:6443 --certificate-authority=/kube-material/ca.crt --embed-certs=true
/bin/kubectl config --kubeconfig "$config" set-credentials scoped --token="$(cat /kube-material/scoped-token)"
/bin/kubectl config --kubeconfig "$config" set-context fixture --cluster=fixture --user=scoped --namespace=fixture-allowed
/bin/kubectl config --kubeconfig "$config" use-context fixture
chmod 644 /kube-material/ca.crt
chmod 600 "$config" /kube-material/observer-token /kube-material/scoped-token
chown 1000:1000 "$config"
touch /run/kube/ready
wait "$server"
