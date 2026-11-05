FROM golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS build

WORKDIR /src/backend
ENV CGO_ENABLED=1

RUN apt-get update \
	&& apt-get install -y --no-install-recommends libssl-dev \
	&& rm -rf /var/lib/apt/lists/*

COPY backend/go.mod backend/go.sum* ./
COPY third_party /src/third_party
RUN go mod download

COPY backend/cmd ./cmd
COPY backend/internal ./internal

RUN go build -o /out/aipermission ./cmd/aipermission

FROM node:24.22.2-bookworm-slim@sha256:01d566e8d94d8c619591d78652babfccd3c6992a3dffa9ed5ab39e11cf6c70e9 AS verification-node

FROM build AS test

# Contributor-only verification dependencies; production does not inherit them.
COPY --from=verification-node /usr/local/bin/node /usr/local/bin/node
ARG TARGETARCH
RUN test "$TARGETARCH" = amd64 \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl \
	&& curl --fail --location --silent --show-error --max-time 120 \
		-o /tmp/kubectl https://dl.k8s.io/release/v1.37.0/bin/linux/amd64/kubectl \
	&& echo '6129359f4e1f3848a5572ccb0b26cf28b8ca08cef38c95a765b2f64a2c961a2f  /tmp/kubectl' | sha256sum --check \
	&& install -m 755 /tmp/kubectl /usr/local/bin/kubectl \
	&& rm /tmp/kubectl \
	&& rm -rf /var/lib/apt/lists/*

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

WORKDIR /app

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl gnupg gosu libpcre2-8-0 libssl3 \
	&& install -d /usr/share/postgresql-common/pgdg \
	&& curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
		| gpg --dearmor -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg \
	&& echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
		> /etc/apt/sources.list.d/pgdg.list \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends postgresql-client-14 postgresql-client-15 postgresql-client-16 postgresql-client-17 postgresql-client-18 \
	&& apt-get purge -y --auto-remove curl gnupg \
	&& dpkg --compare-versions "$(dpkg-query -W -f='${Version}' libpcre2-8-0)" ge "10.42-1+deb12u2" \
	&& rm -rf /var/lib/apt/lists/* \
	&& useradd --system --uid 10001 --home /app --shell /usr/sbin/nologin aipermission \
	&& mkdir -p /app/data \
	&& chown -R aipermission:nogroup /app

COPY --from=build /out/aipermission /usr/local/bin/aipermission
COPY backend/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
CMD ["aipermission"]
