package s3connector

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestS3ListingsDecodeResourceNamesOnceAndKeepOpaqueIdentities(t *testing.T) {
	for _, key := range []string{"folder/a+b %2F\x01.txt", "folder/\u0131\u015f\u0131k.txt", " /a//../ "} {
		t.Run(key, func(t *testing.T) {
			cursor := "opaque+%2F=="
			versionID := "version+%2F=="
			encoded := url.PathEscape(key)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if q.Get("encoding-type") != "url" || q.Get("prefix") != key {
					t.Errorf("request resource encoding/prefix = %v", q)
				}
				if q.Has("versions") {
					if q.Get("key-marker") != key || q.Get("version-id-marker") != versionID {
						t.Errorf("version markers = %v", q)
					}
					_, _ = fmt.Fprintf(w, `<ListVersionsResult><EncodingType>url</EncodingType><NextKeyMarker>%s</NextKeyMarker><NextVersionIdMarker>%s</NextVersionIdMarker><Version><Key>%s</Key><VersionId>%s</VersionId></Version><DeleteMarker><Key>%s</Key><VersionId>%s</VersionId></DeleteMarker></ListVersionsResult>`, encoded, versionID, encoded, versionID, encoded, versionID)
					return
				}
				if q.Get("continuation-token") != cursor {
					t.Errorf("opaque cursor = %q", q.Get("continuation-token"))
				}
				_, _ = fmt.Fprintf(w, `<ListBucketResult><EncodingType>url</EncodingType><Prefix>%s</Prefix><NextContinuationToken>%s</NextContinuationToken><Contents><Key>%s</Key></Contents><CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes></ListBucketResult>`, encoded, cursor, encoded, encoded)
			}))
			defer server.Close()
			client, err := newS3Client(t.Context(), s3TestRuntime(t, server.URL))
			if err != nil {
				t.Fatal(err)
			}
			objects, err := client.ListObjects(t.Context(), key, cursor, 10, true)
			if err != nil {
				t.Fatal(err)
			}
			if objects.Prefix != key || len(objects.Contents) != 1 || objects.Contents[0].Key != key || len(objects.CommonPrefixes) != 1 || objects.CommonPrefixes[0].Prefix != key || objects.NextContinuationToken != cursor {
				t.Fatalf("object identities changed: %#v", objects)
			}
			versions, err := client.ListObjectVersions(t.Context(), key, s3VersionCursor{KeyMarker: key, VersionIDMarker: versionID}, 10)
			if err != nil {
				t.Fatal(err)
			}
			if versions.NextKeyMarker != key || versions.NextVersionIDMarker != versionID || len(versions.Versions) != 1 || versions.Versions[0].Key != key || versions.Versions[0].VersionID != versionID || len(versions.DeleteMarkers) != 1 || versions.DeleteMarkers[0].Key != key || versions.DeleteMarkers[0].VersionID != versionID {
				t.Fatalf("version identities changed: %#v", versions)
			}
		})
	}
}

func TestS3ListingsValidateDeclaredEncodingAndPreserveUnencodedNames(t *testing.T) {
	for _, test := range []struct {
		encoding, name string
		valid          bool
	}{
		{"", "raw+%2F", true}, {"url", "raw+%252F", true}, {"url", "bad%Q0", false}, {"url", "%ff", false}, {"base64", "plain", false},
	} {
		t.Run(test.encoding+":"+test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("versions") {
					_, _ = fmt.Fprintf(w, `<ListVersionsResult><EncodingType>%s</EncodingType><Version><Key>%s</Key><VersionId>opaque%%Q0</VersionId></Version></ListVersionsResult>`, test.encoding, test.name)
				} else {
					_, _ = fmt.Fprintf(w, `<ListBucketResult><EncodingType>%s</EncodingType><Contents><Key>%s</Key></Contents></ListBucketResult>`, test.encoding, test.name)
				}
			}))
			defer server.Close()
			client, err := newS3Client(t.Context(), s3TestRuntime(t, server.URL))
			if err != nil {
				t.Fatal(err)
			}
			objects, objectErr := client.ListObjects(t.Context(), "", "", 10, false)
			versions, versionErr := client.ListObjectVersions(t.Context(), "", s3VersionCursor{}, 10)
			if (objectErr == nil) != test.valid || (versionErr == nil) != test.valid {
				t.Fatalf("encoding acceptance: objects=%v versions=%v", objectErr, versionErr)
			}
			if test.valid && (objects.Contents[0].Key != "raw+%2F" || versions.Versions[0].Key != "raw+%2F" || versions.Versions[0].VersionID != "opaque%Q0") {
				t.Fatalf("keys/opaque version identity changed: %#v %#v", objects, versions)
			}
		})
	}
}
