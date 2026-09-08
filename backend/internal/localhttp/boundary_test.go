package localhttp

import "testing"

func TestLocalBrowserBoundaryParsing(t *testing.T) {
	for _, test := range []struct {
		name string
		got  bool
		want bool
	}{
		{name: "localhost host", got: IsLocalhostHeader("localhost:3211"), want: true},
		{name: "ipv6 host", got: IsLocalhostHeader("[::1]:3211"), want: true},
		{name: "remote host", got: IsLocalhostHeader("192.0.2.1:3211"), want: false},
		{name: "local address", got: IsLocalRemoteAddr("127.0.0.1:12345"), want: true},
		{name: "remote address", got: IsLocalRemoteAddr("192.0.2.1:12345"), want: false},
		{name: "same origin", got: IsSameOrigin("http://localhost:3211", "localhost:3211"), want: true},
		{name: "different loopback port", got: IsSameOrigin("http://localhost:3210", "localhost:3211"), want: false},
		{name: "origin path", got: IsSameOrigin("http://localhost:3211/path", "localhost:3211"), want: false},
		{name: "same-origin referer", got: IsSameOriginReferer("http://localhost:3211/migrate", "localhost:3211"), want: true},
		{name: "remote referer", got: IsSameOriginReferer("https://example.com/migrate", "localhost:3211"), want: false},
		{name: "remote origin", got: IsSameOrigin("https://example.com", "localhost:3211"), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("result = %v, want %v", test.got, test.want)
			}
		})
	}
}
