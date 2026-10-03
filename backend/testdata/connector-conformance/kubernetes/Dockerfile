FROM rancher/k3s:v1.34.1-k3s1@sha256:5e0707cfd1239b358ef73f3254bc3eadc027dd30cd5ec6ca41e29e47652a1b8c

COPY backend/testdata/connector-conformance/kubernetes /fixture
ENTRYPOINT ["/bin/sh", "/fixture/start.sh"]
