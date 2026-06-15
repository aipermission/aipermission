FROM golang:1.26.4-bookworm@sha256:5d2b868674b57c9e48cdd39e891acce4196b6926ca6d11e9c270a8f85106203d AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN go build -o /out/aipermission ./cmd/aipermission

FROM debian:bookworm-slim@sha256:96e378d7e6531ac9a15ad505478fcc2e69f371b10f5cdf87857c4b8188404716

WORKDIR /app

RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates gosu \
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
