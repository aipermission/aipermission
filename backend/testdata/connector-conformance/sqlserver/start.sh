#!/bin/sh
set -eu
umask 077
openssl req -x509 -newkey rsa:2048 -nodes -days 2 \
  -subj '/CN=sqlserver' -addext 'subjectAltName=DNS:sqlserver' \
  -addext 'extendedKeyUsage=serverAuth' \
  -keyout /var/opt/mssql/fixture.key -out /var/opt/mssql/fixture.crt 2>/dev/null
cp /var/opt/mssql/fixture.crt /sqlserver-material/ca.crt
chmod 644 /sqlserver-material/ca.crt
exec /opt/mssql/bin/sqlservr
