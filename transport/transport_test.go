package transport

import (
	"net/http"
	"strings"
	"testing"
)

func TestDumpRequestForTraceRedactsAuthorization(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://cloud.example/token", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer secret-token")

	dump, err := dumpRequestForTrace(request, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(dump), "secret-token") || !strings.Contains(string(dump), "Authorization: [REDACTED]") {
		t.Fatalf("trace dump did not redact Authorization: %s", dump)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer secret-token" {
		t.Fatalf("outbound Authorization = %q", got)
	}
}
