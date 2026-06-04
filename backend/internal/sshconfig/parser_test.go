package sshconfig

import "testing"

func TestParseSSHConfigDiscoversConcreteHosts(t *testing.T) {
	entries := Parse(`
Host worker-1 worker-2
  HostName 10.0.0.5
  User ubuntu
  Port 2222
  IdentityFile ~/.ssh/id_ed25519
  ProxyJump bastion

Host *
  User ignored

Host internal-*
  HostName ignored.example.com

Host quoted
  HostName "example.com" # comment
  User root
  ProxyCommand ssh bastion nc %h %p
`)

	if len(entries) != 3 {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if entries[0].Alias != "worker-1" || entries[0].Host != "10.0.0.5" || entries[0].Username != "ubuntu" || entries[0].Port != 2222 {
		t.Fatalf("unexpected first entry: %#v", entries[0])
	}
	if entries[1].Alias != "worker-2" || entries[1].IdentityFile != "~/.ssh/id_ed25519" || entries[1].ProxyJump != "bastion" {
		t.Fatalf("unexpected second entry: %#v", entries[1])
	}
	if entries[2].Alias != "quoted" || entries[2].Host != "example.com" || entries[2].Port != 22 {
		t.Fatalf("unexpected quoted entry: %#v", entries[2])
	}
	if !entries[2].ProxyCommandConfigured || len(entries[2].Warnings) == 0 {
		t.Fatalf("expected ProxyCommand token warning: %#v", entries[2])
	}
}

func TestParseSSHConfigHandlesEqualsSyntax(t *testing.T) {
	entries := Parse(`
Host prod
  HostName=prod.example.com
  User=deploy
  Port=2200
`)
	if len(entries) != 1 {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if entries[0].Host != "prod.example.com" || entries[0].Username != "deploy" || entries[0].Port != 2200 {
		t.Fatalf("unexpected entry: %#v", entries[0])
	}
}

func TestParseSSHConfigAppliesGlobalHostDefaults(t *testing.T) {
	entries := Parse(`
Host *
  User root
  Port 2200
  IdentityFile ~/.ssh/id_ed25519_main

Host example
  HostName 1.2.3.4
`)

	if len(entries) != 1 {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if entries[0].Alias != "example" || entries[0].Host != "1.2.3.4" {
		t.Fatalf("unexpected host entry: %#v", entries[0])
	}
	if entries[0].Username != "root" || entries[0].Port != 2200 || entries[0].IdentityFile != "~/.ssh/id_ed25519_main" {
		t.Fatalf("global defaults were not applied: %#v", entries[0])
	}
}

func TestParseSSHConfigAppliesBottomGlobalHostDefaults(t *testing.T) {
	entries := Parse(`
Host worker
  HostName 10.0.0.42

Host *
  User ubuntu
  Port 2022
  IdentityFile ~/.ssh/id_worker
`)

	if len(entries) != 1 {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if entries[0].Username != "ubuntu" || entries[0].Port != 2022 || entries[0].IdentityFile != "~/.ssh/id_worker" {
		t.Fatalf("bottom defaults were not applied: %#v", entries[0])
	}
}

func TestParseSSHConfigConcreteHostWinsOverGlobalDefaults(t *testing.T) {
	entries := Parse(`
Host worker
  HostName 10.0.0.42
  User deploy

Host *
  User ubuntu
  Port 2022
`)

	if len(entries) != 1 {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if entries[0].Username != "deploy" || entries[0].Port != 2022 {
		t.Fatalf("concrete host should win over defaults while missing fields are filled: %#v", entries[0])
	}
}

func TestParseSSHConfigFirstValueWinsWhenGlobalComesFirst(t *testing.T) {
	entries := Parse(`
Host *
  User ubuntu
  Port 2022
  IdentityFile ~/.ssh/id_default

Host worker
  HostName 10.0.0.42
  User deploy
  Port 22
  IdentityFile ~/.ssh/id_worker
`)

	if len(entries) != 1 {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if entries[0].Username != "ubuntu" || entries[0].Port != 2022 || entries[0].IdentityFile != "~/.ssh/id_default" {
		t.Fatalf("first matching values should win: %#v", entries[0])
	}
}
