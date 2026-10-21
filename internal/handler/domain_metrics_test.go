package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/drywaters/dejaview/internal/metrics"
)

// Check every sample in a family, including the absence of unwanted results.
func assertDomainMetrics(t *testing.T, m *metrics.Metrics, family string, want map[string]float64) {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status %d", rec.Code)
	}
	got := map[string]float64{}
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if !strings.HasPrefix(line, family+"{") {
			continue
		}
		fields := strings.Fields(line)
		n, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		got[strings.TrimPrefix(fields[0], family)] = n
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: got %v, want %v", family, got, want)
	}
}
