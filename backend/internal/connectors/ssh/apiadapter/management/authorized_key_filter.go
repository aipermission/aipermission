package management

// authorized_keys has an optional options field with quoted spaces before its
// type/blob fields. Backslashes escape only a following quote, as in OpenSSH's
// option scanner. Unsupported matching options fail closed before file publish.
const authorizedKeyIdentityFilter = `
function next_field(line, start, quoted, ch, field) {
  while (cursor <= length(line) && substr(line, cursor, 1) ~ /[ \t]/) cursor++
  start = cursor
  quoted = 0
  while (cursor <= length(line)) {
    ch = substr(line, cursor, 1)
    if (ch == "\\" && substr(line, cursor + 1, 1) == "\"") {
      cursor++
    } else if (ch == "\"") {
      quoted = !quoted
    } else if (!quoted && ch ~ /[ \t]/) {
      break
    }
    cursor++
  }
  if (quoted) { ambiguous = 1; return "" }
  field = substr(line, start, cursor - start)
  if (index(field, "\r")) ambiguous = 1
  return field
}
function supported_options(options, pos, name, ch, closed) {
  pos = 1
  while (pos <= length(options)) {
    name = substr(options, pos)
    if (!match(name, /^[A-Za-z0-9-]+/)) return 0
    name = tolower(substr(name, 1, RLENGTH))
    pos += RLENGTH
    ch = substr(options, pos, 1)
    if (ch == "=") {
      if (name !~ /^(command|principals|from|expiry-time|environment|permitopen|permitlisten|tunnel)$/) return 0
      pos++
      if (substr(options, pos, 1) != "\"") return 0
      closed = 0
      for (pos++; pos <= length(options); pos++) {
        ch = substr(options, pos, 1)
        if (ch == "\\" && substr(options, pos + 1, 1) == "\"") pos++
        else if (ch == "\"") { pos++; closed = 1; break }
      }
      if (!closed) return 0
    } else if (name !~ /^(restrict|cert-authority|(no-)?(port-forwarding|agent-forwarding|x11-forwarding|touch-required|verify-required|pty|user-rc))$/) return 0
    if (pos > length(options)) return 1
    if (substr(options, pos, 1) != ",") return 0
    if (++pos > length(options)) return 0
  }
  return 0
}
BEGIN { removed = 0 }
{
  line = $0
  sub(/\r$/, "", line)
  if (line ~ /^[ \t]*#/) { print; next }
  cursor = 1
  options = ""
  type = next_field(line)
  if (type != key_type) { options = type; type = next_field(line) }
  if (type != key_type) { print; next }
  blob = next_field(line)
  if (type == key_type && blob == key_blob) {
    if (options != "" && !supported_options(options)) { ambiguous = 1; print }
    else removed++
  } else {
    print
  }
}
END {
  if (ambiguous) exit 65
  print removed > "/dev/stderr"
}
`
