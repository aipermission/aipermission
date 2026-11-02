FROM mcr.microsoft.com/mssql/server:2022-latest@sha256:cf8f0ab2ebe61feec2eed2629061a07d247f791c2a8ddda74a4ac9f06a291718
USER root
RUN mkdir -p /sqlserver-material && chown 10001:10001 /sqlserver-material
COPY --chown=10001:10001 backend/testdata/connector-conformance/sqlserver /fixture
COPY --chown=10001:10001 backend/testdata/connector-conformance/sqlserver/mssql.conf /var/opt/mssql/mssql.conf
USER 10001
CMD ["/bin/sh", "/fixture/start.sh"]
