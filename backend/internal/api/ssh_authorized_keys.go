package api

import "strings"

func removeAuthorizedKeyCommand(publicKey string) string {
	blob := publicKeyBlob(publicKey)
	delimiter := "__AIPERMISSION_AUTHORIZED_KEY__"
	for strings.Contains(blob, "\n"+delimiter+"\n") {
		delimiter += "_X"
	}
	return `set -e
KEY_BLOB="$(cat <<'` + delimiter + `'
` + blob + `
` + delimiter + `
)"
if [ -z "$KEY_BLOB" ]; then
  echo "remote key uninstall failed: invalid public key" >&2
  exit 1
fi
mkdir -p ~/.ssh
touch ~/.ssh/authorized_keys
chmod 700 ~/.ssh
tmp="$HOME/.ssh/authorized_keys.aipermission.$$"
awk -v key_blob="$KEY_BLOB" '
BEGIN { removed = 0 }
{
  keep = 1
  for (i = 1; i <= NF; i++) {
    if ($i == key_blob) {
      keep = 0
      removed++
      break
    }
  }
  if (keep) print
}
END { print removed > "/dev/stderr" }
' ~/.ssh/authorized_keys 2>"$tmp.count" > "$tmp"
removed="$(cat "$tmp.count" 2>/dev/null || printf '0')"
rm -f "$tmp.count"
if [ "${removed:-0}" -eq 0 ]; then
  rm -f "$tmp"
  echo "remote key uninstall removed 0 authorized_keys entries" >&2
  exit 1
fi
cat "$tmp" > ~/.ssh/authorized_keys
rm -f "$tmp"
chmod 600 ~/.ssh/authorized_keys
printf 'aipermission_key_removed=%s\n' "$removed"`
}

func publicKeyBlob(publicKey string) string {
	fields := strings.Fields(publicKey)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}
