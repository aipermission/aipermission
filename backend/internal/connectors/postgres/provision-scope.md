# Managed Grant Scope Identity

Schema, table and column names returned by PostgreSQL are opaque resource
identities, not SQL fragments or names that may be cleaned for display. The
provisioning picker retains exact string values, including leading/trailing
whitespace, punctuation, quotes, commas and column order. Similar names must
not collapse into one selection.

The backend accepts nonempty UTF-8 names without NUL, at most 63 bytes for its
supported PostgreSQL identifier contract. Longer names reject instead of
allowing the server to truncate a GRANT target. All selected names are quoted
as SQL identifiers; embedded quotes are doubled. A name resembling SQL text
remains one quoted identifier, never an executable clause.

The shared HTTP decoder rejects malformed Unicode before object-form input
can lose its identity. Scope JSON text uses the same raw JSON validator before
decoding. Valid surrogate pairs and a literal Unicode replacement character
remain valid names. The picker rejects unpaired UTF-16 metadata code units.

Catalog comparisons use explicit escape string literals, quoting backslashes
and apostrophes independently of `standard_conforming_strings`. PL/pgSQL blocks
use a delimiter absent from their body; identifier quoting alone does not
protect an outer dollar-quoted block from names containing its delimiter.

Column selection must be an actual array of strings. Do not coerce numbers,
objects or nulls, trim entries, or infer multiple identifiers by splitting text
on commas. The metadata query returns a JSON column array. Malformed column
metadata does not invent grantable names.

Frontend selection maps use own properties, so database names such as
`__proto__`, `constructor` or `toString` cannot select inherited object members.
Backend SQL construction and the frontend preview must describe the same exact
schema/table/column selection. New role names retain the separate conservative
simple-identifier creation policy; this contract concerns existing resources.
