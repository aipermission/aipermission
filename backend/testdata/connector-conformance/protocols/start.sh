#!/bin/sh
set -eu
umask 077
mkdir -p /run/sshd /run/protocols /fixture-material /home/aipermission/.ssh
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj '/CN=protocols' -addext 'subjectAltName=DNS:protocols' \
  -keyout /run/protocols/tls.key -out /run/protocols/tls.crt 2>/dev/null
cp /run/protocols/tls.crt /fixture-material/ca.crt
chmod 644 /fixture-material/ca.crt
ssh-keygen -q -t ed25519 -N '' -f /run/protocols/ssh_host
ssh-keygen -q -t ed25519 -N '' -f /fixture-material/ssh_client
cp /run/protocols/ssh_host.pub /fixture-material/ssh_host.pub
cp /fixture-material/ssh_client.pub /home/aipermission/.ssh/authorized_keys
chown -R aipermission:aipermission /home/aipermission/.ssh
cp /fixture/dovecot.conf /etc/dovecot/dovecot.conf
cp /fixture/postfix.cf /etc/postfix/main.cf
# Only owned local recipient mailboxes can be delivered; no outbound relay.
postconf -M 'submission/inet=submission inet n - n - - smtpd'
postconf -M 'submissions/inet=submissions inet n - n - - smtpd'
postconf -P 'submissions/inet/smtpd_tls_wrappermode=yes'
dovecot -F &
dovecot_pid=$!
/usr/sbin/sshd -D -e -f /fixture/sshd.conf &
ssh_pid=$!
postfix start-fg &
postfix_pid=$!
cleanup() {
  trap - EXIT INT TERM
  kill "$dovecot_pid" "$ssh_pid" "$postfix_pid" 2>/dev/null || true
  wait "$dovecot_pid" "$ssh_pid" "$postfix_pid" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
while kill -0 "$dovecot_pid" && kill -0 "$ssh_pid" && kill -0 "$postfix_pid"; do
  sleep 1
done
exit 1
