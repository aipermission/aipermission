#!/bin/sh
set -e
umask 077

mkdir -p /app/data
[ ! -L /app/data ] || { echo "data directory must not be a symlink" >&2; exit 1; }
chown -R aipermission:nogroup /app/data
chmod 0700 /app/data

exec gosu aipermission "$@"
