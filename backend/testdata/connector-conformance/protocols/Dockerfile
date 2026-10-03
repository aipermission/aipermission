FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

RUN printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d \
    && chmod 755 /usr/sbin/policy-rc.d \
    && apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      openssh-server openssl dovecot-imapd dovecot-lmtpd postfix \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /bin/sh aipermission \
    && useradd --create-home --shell /usr/sbin/nologin recipient \
    && usermod --password '*' aipermission

COPY backend/testdata/connector-conformance/protocols /fixture
CMD ["sh", "/fixture/start.sh"]
