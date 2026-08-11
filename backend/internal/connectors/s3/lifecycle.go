package s3connector

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	defaultLifecycleRuleID = "aipermission-expiration"
	maxLifecycleRuleID     = 255
	maxLifecycleDays       = 36500
	maxLifecyclePrefix     = 1024
	maxLifecycleResponse   = 128 << 10
)

type s3LifecycleConfiguration struct {
	XMLName xml.Name          `xml:"LifecycleConfiguration"`
	XMLNS   string            `xml:"xmlns,attr,omitempty"`
	Rules   []s3LifecycleRule `xml:"Rule"`
}

type s3LifecycleRule struct {
	ID                          string                            `xml:"ID"`
	Status                      string                            `xml:"Status"`
	Filter                      s3LifecycleFilter                 `xml:"Filter"`
	LegacyPrefix                string                            `xml:"Prefix,omitempty"`
	Expiration                  *s3LifecycleExpiration            `xml:"Expiration,omitempty"`
	NoncurrentVersionExpiration *s3NoncurrentVersionExpiration    `xml:"NoncurrentVersionExpiration,omitempty"`
	AbortIncompleteMultipart    *s3AbortIncompleteMultipartUpload `xml:"AbortIncompleteMultipartUpload,omitempty"`
}

type s3LifecycleFilter struct {
	Prefix string `xml:"Prefix"`
}

type s3LifecycleExpiration struct {
	Days int `xml:"Days"`
}

type s3NoncurrentVersionExpiration struct {
	Days int `xml:"NoncurrentDays"`
}

type s3AbortIncompleteMultipartUpload struct {
	Days int `xml:"DaysAfterInitiation"`
}

func executeGetBucketLifecycle(ctx context.Context, client *s3Client) (connectors.ActionResult, error) {
	configuration, raw, configured, err := client.GetBucketLifecycle(ctx)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	rules := make([]map[string]any, 0, len(configuration.Rules))
	for _, rule := range configuration.Rules {
		prefix := rule.Filter.Prefix
		if prefix == "" {
			prefix = rule.LegacyPrefix
		}
		rules = append(rules, map[string]any{
			"id":                              rule.ID,
			"status":                          rule.Status,
			"prefix":                          prefix,
			"expire_current_after_days":       lifecycleDays(rule.Expiration),
			"expire_noncurrent_after_days":    noncurrentLifecycleDays(rule.NoncurrentVersionExpiration),
			"abort_incomplete_multipart_days": abortMultipartDays(rule.AbortIncompleteMultipart),
		})
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"bucket":     client.bucket,
			"configured": configured,
			"rule_count": len(rules),
			"rules":      rules,
			"raw_xml":    string(raw),
		},
		DisplayText: fmt.Sprintf("Read lifecycle policy for %s (%d rule(s)).", client.bucket, len(rules)),
	}, nil
}

func executeReplaceBucketLifecycle(ctx context.Context, client *s3Client, input map[string]any) (connectors.ActionResult, error) {
	rule := lifecycleRuleFromInput(input)
	configuration := s3LifecycleConfiguration{
		XMLNS: "http://s3.amazonaws.com/doc/2006-03-01/",
		Rules: []s3LifecycleRule{rule},
	}
	if err := client.PutBucketLifecycle(ctx, configuration); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"bucket":                          client.bucket,
			"replaced":                        true,
			"rule_id":                         rule.ID,
			"prefix":                          rule.Filter.Prefix,
			"expire_current_after_days":       lifecycleDays(rule.Expiration),
			"expire_noncurrent_after_days":    noncurrentLifecycleDays(rule.NoncurrentVersionExpiration),
			"abort_incomplete_multipart_days": abortMultipartDays(rule.AbortIncompleteMultipart),
		},
		DisplayText: fmt.Sprintf("Replaced the lifecycle policy for %s with rule %s.", client.bucket, rule.ID),
	}, nil
}

func executeDeleteBucketLifecycle(ctx context.Context, client *s3Client) (connectors.ActionResult, error) {
	if err := client.DeleteBucketLifecycle(ctx); err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"bucket":  client.bucket,
			"deleted": true,
		},
		DisplayText: fmt.Sprintf("Deleted the lifecycle policy for %s.", client.bucket),
	}, nil
}

func (client *s3Client) GetBucketLifecycle(ctx context.Context) (s3LifecycleConfiguration, []byte, bool, error) {
	data, _, err := client.Do(ctx, http.MethodGet, "", url.Values{"lifecycle": []string{""}}, nil, maxLifecycleResponse)
	if err != nil {
		if isNotFoundError(err) || strings.Contains(strings.ToLower(err.Error()), "nosuchlifecycleconfiguration") {
			return s3LifecycleConfiguration{}, nil, false, nil
		}
		return s3LifecycleConfiguration{}, nil, false, err
	}
	var configuration s3LifecycleConfiguration
	if err := xml.Unmarshal(data, &configuration); err != nil {
		return s3LifecycleConfiguration{}, nil, false, fmt.Errorf("decode s3 lifecycle response: %w", err)
	}
	return configuration, data, true, nil
}

func (client *s3Client) PutBucketLifecycle(ctx context.Context, configuration s3LifecycleConfiguration) error {
	data, err := xml.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("encode s3 lifecycle request: %w", err)
	}
	headers := http.Header{"Content-Type": []string{"application/xml"}}
	checksum := md5.Sum(data) // S3 requires Content-MD5 as a transport integrity checksum.
	headers.Set("Content-MD5", base64.StdEncoding.EncodeToString(checksum[:]))
	_, _, err = client.Do(ctx, http.MethodPut, "", url.Values{"lifecycle": []string{""}}, s3RequestBody{Headers: headers, Data: data}, maxLifecycleResponse)
	return err
}

func (client *s3Client) DeleteBucketLifecycle(ctx context.Context) error {
	_, _, err := client.Do(ctx, http.MethodDelete, "", url.Values{"lifecycle": []string{""}}, nil, maxLifecycleResponse)
	return err
}

func lifecycleRuleFromInput(input map[string]any) s3LifecycleRule {
	rule := s3LifecycleRule{
		ID:     strings.TrimSpace(stringValue(input, "rule_id")),
		Status: "Enabled",
		Filter: s3LifecycleFilter{Prefix: normalizeObjectPrefix(stringValue(input, "prefix"))},
	}
	if !boolValue(input, "enabled") {
		rule.Status = "Disabled"
	}
	if days := intValue(input, "expire_current_after_days"); days > 0 {
		rule.Expiration = &s3LifecycleExpiration{Days: days}
	}
	if days := intValue(input, "expire_noncurrent_after_days"); days > 0 {
		rule.NoncurrentVersionExpiration = &s3NoncurrentVersionExpiration{Days: days}
	}
	if days := intValue(input, "abort_incomplete_multipart_days"); days > 0 {
		rule.AbortIncompleteMultipart = &s3AbortIncompleteMultipartUpload{Days: days}
	}
	return rule
}

func validateLifecycleInput(input map[string]any) error {
	ruleID := strings.TrimSpace(stringValue(input, "rule_id"))
	if ruleID == "" {
		return fmt.Errorf("rule_id is required")
	}
	if len(ruleID) > maxLifecycleRuleID {
		return fmt.Errorf("rule_id is too large")
	}
	if len(normalizeObjectPrefix(stringValue(input, "prefix"))) > maxLifecyclePrefix {
		return fmt.Errorf("prefix is too large")
	}
	fields := []string{"expire_current_after_days", "expire_noncurrent_after_days", "abort_incomplete_multipart_days"}
	configured := false
	for _, field := range fields {
		days := intValue(input, field)
		if days < 0 || days > maxLifecycleDays {
			return fmt.Errorf("%s must be between 0 and %d days", field, maxLifecycleDays)
		}
		configured = configured || days > 0
	}
	if !configured {
		return fmt.Errorf("configure at least one lifecycle day value")
	}
	return nil
}

func normalizeObjectPrefix(value string) string {
	return strings.TrimLeft(strings.TrimSpace(value), "/")
}

func lifecycleDays(value *s3LifecycleExpiration) int {
	if value == nil {
		return 0
	}
	return value.Days
}

func noncurrentLifecycleDays(value *s3NoncurrentVersionExpiration) int {
	if value == nil {
		return 0
	}
	return value.Days
}

func abortMultipartDays(value *s3AbortIncompleteMultipartUpload) int {
	if value == nil {
		return 0
	}
	return value.Days
}
