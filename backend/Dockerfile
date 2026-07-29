FROM golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN go build -o /out/aipermission ./cmd/aipermission

FROM debian:bookworm-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df

WORKDIR /app

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl gnupg gosu \
	&& install -d /usr/share/postgresql-common/pgdg \
	&& curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
		| gpg --dearmor -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg \
	&& echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
		> /etc/apt/sources.list.d/pgdg.list \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends postgresql-client-18 \
	&& apt-get purge -y --auto-remove curl gnupg \
	&& rm -rf /var/lib/apt/lists/* \
	&& useradd --system --uid 10001 --home /app --shell /usr/sbin/nologin aipermission \
	&& mkdir -p /app/data \
	&& chown -R aipermission:nogroup /app

COPY --from=build /out/aipermission /usr/local/bin/aipermission
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
CMD ["aipermission"]
