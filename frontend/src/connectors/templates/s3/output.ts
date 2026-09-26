export function s3OutputRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function text(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function stringMap(value: unknown): Record<string, string> {
  return Object.fromEntries(
    Object.entries(s3OutputRecord(value)).filter((entry): entry is [string, string] => typeof entry[1] === "string"),
  );
}

export function readS3Objects(value: unknown) {
  if (!Array.isArray(value)) return [];
  return value.flatMap((raw: unknown) => {
    const object = s3OutputRecord(raw);
    return typeof object.key === "string"
      ? [
          {
            ...object,
            key: object.key,
            size: typeof object.size === "number" || typeof object.size === "string" ? object.size : undefined,
            last_modified: text(object.last_modified),
            etag: text(object.etag),
          },
        ]
      : [];
  });
}

export function readS3Directories(value: unknown) {
  if (!Array.isArray(value)) return [];
  return value.flatMap((raw: unknown) => {
    const directory = s3OutputRecord(raw);
    return typeof directory.prefix === "string" ? [{ ...directory, prefix: directory.prefix, name: text(directory.name) }] : [];
  });
}

export function readS3Metadata(value: unknown) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const record = s3OutputRecord(value);
  return {
    ...record,
    key: text(record.key),
    bucket: text(record.bucket),
    endpoint: text(record.endpoint),
    region: text(record.region),
    headers: stringMap(record.headers),
    content_length:
      typeof record.content_length === "number" || typeof record.content_length === "string" ? record.content_length : undefined,
    content_type: text(record.content_type),
    last_modified: text(record.last_modified),
    etag: text(record.etag),
  };
}

export function readS3Presign(value: unknown) {
  const record = s3OutputRecord(value);
  if (typeof record.url !== "string" || !record.url) throw new Error("Invalid S3 presigned URL response.");
  return {
    url: record.url,
    operation: text(record.operation),
    expires_at: text(record.expires_at),
    required_headers: stringMap(record.required_headers),
  };
}

export function readS3Versions(value: unknown) {
  const record = s3OutputRecord(value);
  const versions = Array.isArray(record.versions)
    ? record.versions.flatMap((raw: unknown) => {
        const version = s3OutputRecord(raw);
        for (const field of ["is_latest", "delete_marker"]) {
          if (version[field] !== undefined && typeof version[field] !== "boolean") throw new Error("Invalid S3 object version flags.");
        }
        return typeof version.version_id === "string"
          ? [
              {
                version_id: version.version_id,
                is_latest: version.is_latest === true,
                delete_marker: version.delete_marker === true,
                last_modified: text(version.last_modified),
                size: typeof version.size === "number" ? version.size : undefined,
              },
            ]
          : [];
      })
    : [];
  return { versions, next_cursor: text(record.next_cursor) || "" };
}

export function readS3Lifecycle(value: unknown) {
  const record = s3OutputRecord(value);
  const rules = Array.isArray(record.rules)
    ? record.rules.flatMap((raw: unknown) => {
        const rule = s3OutputRecord(raw);
        if (typeof rule.id !== "string") return [];
        return [
          {
            id: rule.id,
            status: text(rule.status) || "",
            prefix: text(rule.prefix) || "",
            expire_current_after_days: typeof rule.expire_current_after_days === "number" ? rule.expire_current_after_days : 0,
            expire_noncurrent_after_days: typeof rule.expire_noncurrent_after_days === "number" ? rule.expire_noncurrent_after_days : 0,
            abort_incomplete_multipart_days:
              typeof rule.abort_incomplete_multipart_days === "number" ? rule.abort_incomplete_multipart_days : 0,
          },
        ];
      })
    : [];
  return { configured: record.configured === true, rules, raw_xml: text(record.raw_xml) || "" };
}
