.PHONY: help hygiene secret-history-check rest-contract rest-contract-check backend-test backend-race backend-vet backend-vuln connector-conformance frontend-lint frontend-format-check frontend-test frontend-coverage frontend-e2e frontend-build frontend-audit mcp-test mcp-build mcp-audit mcp-pack placeholder-pack test build audit release-check docker-up docker-ps

help:
	@printf '%s\n' \
		'Available targets:' \
		'  make test            Run backend, frontend, and MCP tests' \
		'  make build           Build frontend and MCP package' \
		'  make audit           Run frontend and MCP production audits' \
		'  make hygiene         Run repository security and maintenance checks' \
		'  make secret-history-check  Scan current files and Git history for secrets' \
		'  make rest-contract   Regenerate the incremental typed OpenAPI contract' \
		'  make frontend-lint   Lint frontend source and React hooks' \
		'  make frontend-format-check  Check frontend formatting' \
		'  make frontend-coverage  Enforce critical frontend per-file coverage floors' \
		'  make connector-conformance  Test protocol connectors against disposable real services' \
		'  make release-check   Run the local RC verification set' \
		'  make docker-up       Build and start the local Docker stack'

hygiene:
	npm run hygiene

secret-history-check:
	npm run security:history

rest-contract:
	cd backend && go run ./cmd/openapi -routes internal/api/routes.go -output ../docs/api/openapi.json

rest-contract-check:
	cd backend && go run ./cmd/openapi -routes internal/api/routes.go -output ../docs/api/openapi.json -check

backend-test:
	cd backend && coverage=$$(mktemp) && trap 'rm -f "$$coverage"' EXIT; go test -coverprofile="$$coverage" ./... && go tool cover -func="$$coverage" | tail -1 && go run ./cmd/coveragecheck -profile "$$coverage"

backend-race:
	cd backend && go test -race ./...

backend-vet:
	cd backend && go vet ./...

backend-vuln:
	cd backend && govulncheck ./...

connector-conformance:
	@set -eu; \
		compose='docker compose -p aipermission-conformance -f backend/testdata/connector-conformance/compose.yml'; \
		trap "$$compose down -v --remove-orphans" EXIT; \
		$$compose up -d --wait clickhouse postgres valkey rabbitmq minio; \
		$$compose run --rm minio-init; \
		postgres_port=$$($$compose port postgres 5432 | sed 's/.*://'); \
		valkey_port=$$($$compose port valkey 6379 | sed 's/.*://'); \
		rabbitmq_port=$$($$compose port rabbitmq 15672 | sed 's/.*://'); \
		s3_port=$$($$compose port minio 9000 | sed 's/.*://'); \
		clickhouse_port=$$($$compose port clickhouse 9000 | sed 's/.*://'); \
		(cd backend && \
			AIPERMISSION_CONFORMANCE=1 \
			AIPERMISSION_POSTGRES_PORT="$$postgres_port" \
			AIPERMISSION_VALKEY_PORT="$$valkey_port" \
			AIPERMISSION_RABBITMQ_PORT="$$rabbitmq_port" \
			AIPERMISSION_S3_PORT="$$s3_port" \
			AIPERMISSION_CLICKHOUSE_PORT="$$clickhouse_port" \
			go test ./internal/connectors/conformance -count=1 -v)

frontend-lint:
	cd frontend && npm run lint

frontend-format-check:
	cd frontend && npm run format:check

frontend-test:
	cd frontend && npm test

frontend-coverage:
	cd frontend && npm run test:coverage

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

release-check: hygiene secret-history-check rest-contract-check backend-test backend-race backend-vet backend-vuln frontend-lint frontend-format-check frontend-test frontend-coverage frontend-build frontend-e2e frontend-audit mcp-test mcp-build mcp-audit mcp-pack placeholder-pack

docker-up:
	docker compose up -d --build

docker-ps:
	docker compose ps
