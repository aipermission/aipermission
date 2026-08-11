.PHONY: help hygiene backend-test backend-race backend-vet backend-vuln frontend-lint frontend-format-check frontend-test frontend-e2e frontend-build frontend-audit mcp-test mcp-build mcp-audit mcp-pack placeholder-pack test build audit release-check docker-up docker-ps

help:
	@printf '%s\n' \
		'Available targets:' \
		'  make test            Run backend, frontend, and MCP tests' \
		'  make build           Build frontend and MCP package' \
		'  make audit           Run frontend and MCP production audits' \
		'  make hygiene         Run repository security and maintenance checks' \
		'  make frontend-lint   Lint frontend source and React hooks' \
		'  make frontend-format-check  Check frontend formatting' \
		'  make release-check   Run the local RC verification set' \
		'  make docker-up       Build and start the local Docker stack'

hygiene:
	npm run hygiene

backend-test:
	cd backend && go test ./...

backend-race:
	cd backend && go test -race ./...

backend-vet:
	cd backend && go vet ./...

backend-vuln:
	cd backend && govulncheck ./...

frontend-lint:
	cd frontend && npm run lint

frontend-format-check:
	cd frontend && npm run format:check

frontend-test:
	cd frontend && npm test

frontend-e2e:
	cd frontend && npm run test:e2e

frontend-build:
	cd frontend && npm run build

frontend-audit:
	cd frontend && npm audit --omit=dev --audit-level=moderate

mcp-test:
	cd packages/mcp && npm test

mcp-build:
	cd packages/mcp && npm run build

mcp-audit:
	cd packages/mcp && npm audit --omit=dev --audit-level=moderate

mcp-pack:
	cd packages/mcp && npm pack --dry-run

placeholder-pack:
	cd packages/npm-placeholder && npm pack --dry-run

test: backend-test frontend-test mcp-test

build: frontend-build mcp-build

audit: frontend-audit mcp-audit

release-check: hygiene backend-test backend-race backend-vet backend-vuln frontend-lint frontend-format-check frontend-test frontend-build frontend-e2e frontend-audit mcp-test mcp-build mcp-audit mcp-pack placeholder-pack

docker-up:
	docker compose up -d --build

docker-ps:
	docker compose ps
