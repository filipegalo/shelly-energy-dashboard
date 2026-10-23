package web

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/filipegalo/shelly-energy-dashboard/internal/database"
)

func TestDetectHourlyAnomaliesRequiresCompleteComparableIncrease(t *testing.T) {
	location := time.UTC
	recentStart := time.Date(2026, time.September, 14, 0, 0, 0, 0, location)
	candidate := recentStart.Add(13 * time.Hour)
	aggregates := make([]database.EnergyAggregate, 0, 7)
	for week := 1; week <= 4; week++ {
		at := candidate.AddDate(0, 0, -7*week)
		aggregates = append(aggregates, database.EnergyAggregate{IntervalStart: at, EnergyConsumedWh: 1000, SampleCount: 4, CalculationQuality: "measured"})
	}
	aggregates = append(aggregates,
		database.EnergyAggregate{IntervalStart: candidate, EnergyConsumedWh: 1800, SampleCount: 4, CalculationQuality: "measured"},
		database.EnergyAggregate{IntervalStart: candidate.Add(time.Hour), EnergyConsumedWh: 3000, SampleCount: 4, MissingSampleCount: 1, CalculationQuality: "partial"},
	)

	items := detectHourlyAnomalies(aggregates, recentStart, recentStart.AddDate(0, 0, 7), location)
	if len(items) != 1 {
		t.Fatalf("anomalies = %#v, want one complete high-consumption hour", items)
	}
	if items[0].Actual != "1.80 kWh" || items[0].Typical != "1.00 kWh" || items[0].Increase != "+0.80 kWh" {
		t.Fatalf("anomaly = %#v", items[0])
	}
}

func TestReportsExportDownloadsZipAndSingleCSV(t *testing.T) {
	t.Parallel()
	router, store := testRouter(t)
	defer store.Close()
	seedDashboardEnergyReading(t, store, 850)
	location, _ := time.LoadLocation("Europe/Lisbon")
	today := time.Now().In(location)
	start, end := today.AddDate(0, 0, -2).Format(time.DateOnly), today.AddDate(0, 0, -1).Format(time.DateOnly)
	query := "/reports/export?start=" + start + "&end=" + end + "&report="

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, query+"all", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("zip export = %d %q: %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	prefix := "shelly-energy_" + start + "_to_" + end + "_"
	var names []string
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	if strings.Join(names, ",") != prefix+"README.txt,"+prefix+"interval-15min.csv,"+prefix+"hourly.csv,"+prefix+"daily.csv,"+prefix+"monthly.csv,"+prefix+"electricity-cost.csv" {
		t.Fatalf("zip entries = %v", names)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, query+"daily", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Disposition"), prefix+"daily.csv") || !strings.HasPrefix(response.Body.String(), "date,weekday,data_completeness_pct,household_consumption_kwh") {
		t.Fatalf("daily export = %d %v: %s", response.Code, response.Header(), response.Body.String())
	}
	if lines := strings.Count(response.Body.String(), "\n"); lines != 3 {
		t.Fatalf("daily export lines = %d, want header + 2 days", lines)
	}

	for _, invalid := range []string{query + "unknown", "/reports/export?start=2020-01-01&end=" + end + "&report=daily", "/reports/export?start=bad&report=daily"} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, invalid, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", invalid, response.Code)
		}
	}
}
