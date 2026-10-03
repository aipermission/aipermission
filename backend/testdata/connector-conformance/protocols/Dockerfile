FROM rancher/k3s:v1.34.1-k3s1@sha256:5e0707cfd1239b358ef73f3254bc3eadc027dd30cd5ec6ca41e29e47652a1b8c AS kubectl
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
COPY --from=kubectl /bin/k3s /usr/local/bin/k3s
RUN ln -s /usr/local/bin/k3s /usr/local/bin/kubectl \
    && install -m 755 /fixture/kubectl-contained /usr/local/bin/kubectl-contained
CMD ["sh", "/fixture/start.sh"]
