package projectvault

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

func (s *Store) normalizeCreateInput(ctx context.Context, input CreateInput) (CreateInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.SecretType = strings.TrimSpace(input.SecretType)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Environment = strings.TrimSpace(input.Environment)
	input.Description = strings.TrimSpace(input.Description)
	input.Source = strings.TrimSpace(input.Source)
	input.GeneratorKind = strings.TrimSpace(input.GeneratorKind)
	if err := validateEnvironmentName(input.Name); err != nil {
		return CreateInput{}, err
	}
	if !validSecretTypes[input.SecretType] {
		return CreateInput{}, ValidationError("unsupported secret type")
	}
	if input.OwnerProjectID < 1 {
		return CreateInput{}, ValidationError("owner project is required")
	}
	if input.Source != "imported" && input.Source != "generated" {
		return CreateInput{}, ValidationError("source must be imported or generated")
	}
	if input.Source == "generated" {
		if input.GeneratorKind == "" {
			return CreateInput{}, ValidationError("generator kind is required")
		}
		value, parameters, err := Generate(input.GeneratorKind)
		if err != nil {
			return CreateInput{}, err
		}
		input.Value = value
		input.GeneratorParams = parameters
	} else if input.GeneratorKind != "" {
		return CreateInput{}, ValidationError("imported values cannot specify a generator")
	}
	if err := validateValue(input.Value); err != nil {
		return CreateInput{}, err
	}
	expiresAt, err := normalizeExpiry(input.ExpiresAt)
	if err != nil {
		return CreateInput{}, err
	}
	input.ExpiresAt = expiresAt
	if input.ExpiryWarningDays == 0 {
		input.ExpiryWarningDays = defaultExpiryWarnDays
	}
	if input.ExpiryWarningDays < 1 || input.ExpiryWarningDays > 3650 {
		return CreateInput{}, ValidationError("expiry warning days must be between 1 and 3650")
	}
	if err := validateMetadata(input.Provider, input.Environment, input.Description); err != nil {
		return CreateInput{}, err
	}
	input.SharedProjectIDs, err = normalizeProjectIDs(input.OwnerProjectID, input.SharedProjectIDs)
	if err != nil {
		return CreateInput{}, err
	}
	input.Tags, err = normalizeTags(input.Tags)
	if err != nil {
		return CreateInput{}, err
	}
	input.UsageNotes, err = normalizeUsageNotes(input.UsageNotes)
	if err != nil {
		return CreateInput{}, err
	}
	if err := validateActiveProjects(ctx, s.db, append([]int64{input.OwnerProjectID}, input.SharedProjectIDs...)); err != nil {
		return CreateInput{}, err
	}
	return input, nil
}

const itemSelect = `
	SELECT vi.id, vi.name, vi.owner_project_id, p.name, vi.secret_type, vi.value_mode,
		vi.value_version, vi.metadata_revision, vi.encryption_version, vi.provider,
		vi.environment, vi.description, COALESCE(vi.expires_at, ''), vi.expiry_warning_days,
		vi.last_value_replaced_at, COALESCE(vi.last_used_at, ''), vi.usage_count, vi.source,
		vi.generator_kind, vi.generator_parameters_json, vi.status, vi.created_at, vi.updated_at
	FROM vault_items vi
	JOIN projects p ON p.id = vi.owner_project_id`

func validateEnvironmentName(value string) error {
	if err := sessionenv.ValidateName(value); err != nil {
		return ValidationError(err.Error())
	}
	return nil
}

func validateValue(value string) error {
	if value == "" {
		return ValidationError("value is required")
	}
	if len(value) > maxValueBytes {
		return ValidationError("value exceeds the 16 KiB limit")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return ValidationError("text values cannot contain NUL bytes")
	}
	if !utf8.ValidString(value) {
		return ValidationError("text values must be valid UTF-8")
	}
	return nil
}

func validateMetadata(values ...string) error {
	for _, value := range values {
		if utf8.RuneCountInString(value) > maxMetadataRunes {
			return ValidationError("metadata field is too long")
		}
		for _, r := range value {
			if !unicode.IsPrint(r) && r != '\n' && r != '\t' {
				return ValidationError("metadata contains unsupported characters")
			}
		}
	}
	return nil
}

func normalizeExpiry(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || !parsed.After(time.Now().UTC()) {
		return "", ValidationError("expires_at must be a future RFC3339 timestamp")
	}
	return parsed.UTC().Format(time.RFC3339), nil
}

func normalizeProjectIDs(ownerID int64, values []int64) ([]int64, error) {
	if len(values)+1 > maxProjectsPerItem {
		return nil, ValidationError("too many shared projects")
	}
	seen := map[int64]bool{ownerID: true}
	output := make([]int64, 0, len(values))
	for _, id := range values {
		if id < 1 || seen[id] {
			return nil, ValidationError("shared project ids must be unique and exclude the owner")
		}
		seen[id] = true
		output = append(output, id)
	}
	return output, nil
}

func normalizeTags(values []string) ([]string, error) {
	if len(values) > maxTagsPerItem {
		return nil, ValidationError("too many tags")
	}
	seen := map[string]bool{}
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || utf8.RuneCountInString(value) > 64 || seen[key] {
			return nil, ValidationError("tags must be unique, non-empty, and 64 characters or fewer")
		}
		seen[key] = true
		output = append(output, value)
	}
	return output, nil
}

func normalizeUsageNotes(values []UsageNote) ([]UsageNote, error) {
	if len(values) > maxUsageNotesPerItem {
		return nil, ValidationError("too many usage notes")
	}
	for index := range values {
		values[index].Location = strings.TrimSpace(values[index].Location)
		values[index].Notes = strings.TrimSpace(values[index].Notes)
		if values[index].Location == "" {
			return nil, ValidationError("usage note location is required")
		}
		if err := validateMetadata(values[index].Location, values[index].Notes); err != nil {
			return nil, err
		}
	}
	return values, nil
}
