# Lossless JSON String Identity

Validate bounded raw JSON before decoding it into resource names or other
security-sensitive strings. Go's standard JSON decoder replaces malformed
UTF-8 and unpaired Unicode escapes with U+FFFD. That replacement can silently
select another resource; this owner rejects the original input instead.

The standard JSON parser owns syntax. This validator only checks Unicode
identity, including object keys, valid surrogate pairs and escape parity. A
literal U+FFFD remains valid. Callers still own body limits, typed decoding,
unknown-field policy, numeric precision and connector-specific validation.

HTTP request decoding and nested JSON-text connector inputs use this same
implementation. Do not copy the Unicode scanner into individual connectors.
