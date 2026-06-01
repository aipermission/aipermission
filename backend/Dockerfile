FROM golang:1.26.3-bookworm@sha256:386d475a660466863d9f8c766fec64d7fdad3edac2c6a05020c09534d71edb4b AS build

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN go build -o /out/aipermission ./cmd/aipermission

FROM debian:bookworm-slim@sha256:0104b334637a5f19aa9c983a91b54c89887c0984081f2068983107a6f6c21eeb

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
