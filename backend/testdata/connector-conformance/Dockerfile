FROM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS build

RUN apt-get update \
    && apt-get install -y --no-install-recommends libssl-dev curl gnupg \
    && install -d /usr/share/postgresql-common/pgdg \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
      | gpg --dearmor -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg \
    && echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
      > /etc/apt/sources.list.d/pgdg.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends postgresql-client-14 postgresql-client-15 postgresql-client-16 postgresql-client-17 postgresql-client-18 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/cmd ./cmd
COPY backend/internal ./internal
COPY scripts/verification-policy.json /src/scripts/verification-policy.json
ENV CGO_ENABLED=1 GOTELEMETRY=off GOSUMDB=off GOPROXY=off GOCACHE=/cache \
    GOWORK=off GOENV=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local

FROM build AS guest-test
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c \
    ./internal/connectors/docker/daemonconformance -o /guest.test

FROM --platform=linux/amd64 docker:28.5.1-dind@sha256:ea9d20492ca1caaaba78e68453433895d256173c79281756e88b745647fcbcfd AS guest
RUN apk add --no-cache openssh-server openssh-keygen linux-virt \
    && addgroup -S aipermission && adduser -S -D -u 1000 -s /bin/sh -G aipermission aipermission \
    && addgroup aipermission docker \
    && mkdir -p /fixture /seed/bin /seed/lib /proc /sys /dev /run \
    && cp /bin/busybox /seed/bin/ \
    && cp /lib/ld-musl-x86_64.so.1 /seed/lib/ \
    && ln -s busybox /seed/bin/sh && ln -s busybox /seed/bin/sleep \
    && tar -C /seed -cf /fixture/seed.tar . && rm -rf /seed
COPY --from=guest-test /guest.test /fixture/guest.test
COPY backend/testdata/connector-conformance/docker-vm/ /fixture/
RUN cp /fixture/init /init && chmod 755 /init /fixture/docker-contained

FROM build AS archive
RUN apt-get update && apt-get install -y --no-install-recommends cpio gzip \
    && rm -rf /var/lib/apt/lists/*
COPY --from=guest / /vm-root/
RUN mkdir /docker-vm && cp /vm-root/boot/vmlinuz-virt /docker-vm/kernel \
    && cd /vm-root \
    && find . \( -path './boot' -o -path './lib/modules' -o -path './usr/local/libexec/docker/cli-plugins' \) \
      -prune -o -print0 | cpio -o -0 -H newc | gzip > /docker-vm/initramfs.gz

FROM build AS runner
RUN apt-get update && apt-get install -y --no-install-recommends qemu-system-x86 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=archive /docker-vm /fixtures/docker-vm
