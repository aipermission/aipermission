package s3connector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestLifecycleReadOnlyMissingConfigurationIsEmpty(t *testing.T) {
	cases := []struct {
		name, code, message string
		status              int
		empty               bool
	}{
		{name: "missing policy", status: 404, code: "NoSuchLifecycleConfiguration", empty: true},
		{name: "missing bucket", status: 404, code: "NoSuchBucket"},
		{name: "missing key", status: 404, code: "NoSuchKey"},
		{name: "uncoded not found", status: 404},
		{name: "message is not a code", status: 404, message: "NoSuchLifecycleConfiguration"},
		{name: "wrong code case", status: 404, code: "nosuchlifecycleconfiguration"},
		{name: "prefixed code", status: 404, code: "NotNoSuchLifecycleConfiguration"},
		{name: "permission denial with misleading code", status: 403, code: "NoSuchLifecycleConfiguration"},
		{name: "permission denial with misleading message", status: 403, code: "AccessDenied", message: "NoSuchLifecycleConfiguration"},
		{name: "provider failure with misleading code", status: 503, code: "NoSuchLifecycleConfiguration"},
		{name: "provider failure with misleading message", status: 503, code: "ServiceUnavailable", message: "NoSuchLifecycleConfiguration"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := &lifecycleResponseBody{Reader: strings.NewReader(fmt.Sprintf("<Error><Code>%s</Code><Message>%s</Message></Error>", test.code, test.message))}
			calls := 0
			client := scriptedLifecycleClient(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodGet || request.URL.Path != "/bucket" || request.URL.Query().Get("lifecycle") != "" || !request.URL.Query().Has("lifecycle") {
					t.Fatalf("unexpected lifecycle request: %s %s", request.Method, request.URL)
				}
				return &http.Response{StatusCode: test.status, Header: http.Header{}, Body: body}, nil
			})
			result, err := executeGetBucketLifecycle(t.Context(), client)
			if calls != 1 || !body.closed {
				t.Fatalf("requests=%d body closed=%v", calls, body.closed)
			}
			if test.empty {
				if err != nil || result.Status != connectors.ResultCompleted {
					t.Fatalf("missing policy did not complete: result=%#v err=%v", result, err)
				}
				output := result.Output.(map[string]any)
				if output["configured"] != false || output["rule_count"] != 0 || len(output["rules"].([]map[string]any)) != 0 {
					t.Fatalf("missing policy output: %#v", output)
				}
				return
			}
			var statusErr *s3StatusError
			if !errors.As(err, &statusErr) || statusErr.status != test.status || statusErr.code != test.code || result.Status == connectors.ResultCompleted {
				t.Fatalf("provider error became success or lost identity: result=%#v err=%v", result, err)
			}
			if test.status == http.StatusNotFound && connectors.ErrorCode(err) != "not_found" {
				t.Fatalf("lost not_found classification: %v", err)
			}
		})
	}
}

func TestLifecycleReadPreservesTransportFailures(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("transport failed: NoSuchLifecycleConfiguration")} {
		t.Run(failure.Error(), func(t *testing.T) {
			client := scriptedLifecycleClient(func(*http.Request) (*http.Response, error) { return nil, failure })
			result, err := executeGetBucketLifecycle(t.Context(), client)
			if !errors.Is(err, failure) || result.Status == connectors.ResultCompleted {
				t.Fatalf("lost transport error: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestLifecycleReadValidatesSuccessfulResponse(t *testing.T) {
	for _, test := range []struct {
		name, data string
		valid      bool
	}{
		{name: "valid rule", data: `<LifecycleConfiguration><Rule><ID>expiry</ID><Status>Enabled</Status><Filter><Prefix>archive/</Prefix></Filter><Expiration><Days>30</Days></Expiration></Rule></LifecycleConfiguration>`, valid: true},
		{name: "malformed XML", data: `<LifecycleConfiguration><Rule>`},
		{name: "bounded response", data: strings.Repeat("x", maxLifecycleResponse+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &lifecycleResponseBody{Reader: strings.NewReader(test.data)}
			client := scriptedLifecycleClient(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}, nil
			})
			result, err := executeGetBucketLifecycle(t.Context(), client)
			if !body.closed {
				t.Fatal("response body not closed")
			}
			if !test.valid {
				if err == nil || result.Status == connectors.ResultCompleted {
					t.Fatalf("invalid response became success: result=%#v err=%v", result, err)
				}
				return
			}
			if err != nil || result.Status != connectors.ResultCompleted {
				t.Fatalf("valid response failed: result=%#v err=%v", result, err)
			}
			output := result.Output.(map[string]any)
			rules := output["rules"].([]map[string]any)
			if output["configured"] != true || output["rule_count"] != 1 || output["raw_xml"] != test.data || len(rules) != 1 || rules[0]["expire_current_after_days"] != 30 || rules[0]["prefix"] != "archive/" {
				t.Fatalf("invalid rule projection: %#v", output)
			}
		})
	}
}

func scriptedLifecycleClient(roundTrip s3RoundTripFunc) *s3Client {
	return &s3Client{
		scheme: "http", host: "s3.invalid", port: 80, region: "us-east-1", bucket: "bucket", pathStyle: true,
		accessKey: "access", secretKey: "secret", httpClient: &http.Client{Transport: roundTrip},
	}
}

type lifecycleResponseBody struct {
	io.Reader
	closed bool
}

func (body *lifecycleResponseBody) Close() error { body.closed = true; return nil }
