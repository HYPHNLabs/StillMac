package baseline

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"stillmac/internal/observe"
)

func baselineSample(at string, status string) observe.Sample {
	return observe.Sample{
		SchemaVersion: "stillmac.sample.v1",
		CapturedAt:    at,
		Quality: observe.Quality{
			Valid:                   true,
			Status:                  status,
			MemoryPressureAvailable: true,
			SwapUsedAvailable:       true,
		},
	}
}

func TestBuildEmptyInputCollectsSafely(t *testing.T) {
	got, err := Build(nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.Status != "collecting" || got.ValidSamples != 0 || got.CoverageIntervals != 0 || got.ObservedDays != 0 || got.FirstCapturedAt != "" || got.LastCapturedAt != "" {
		t.Fatalf("empty status = %#v", got)
	}
	if len(got.Blockers) != 3 {
		t.Fatalf("empty blockers = %#v", got.Blockers)
	}
}

func TestBuildRejectsNonCanonicalTimestampWithoutLeakingIt(t *testing.T) {
	secret := "2026-08-01T12:00:00+00:00-private-secret"
	_, err := Build([]observe.Sample{baselineSample(secret, "complete")})
	if err == nil || err.Error() != "invalid sample timestamp" {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error leaked captured_at")
	}
}

func TestBuildOrdersSamplesWithoutMutatingCaller(t *testing.T) {
	input := []observe.Sample{
		baselineSample("2026-08-02T12:00:00Z", "complete"),
		baselineSample("2026-08-01T12:00:00Z", "complete"),
	}
	original := append([]observe.Sample(nil), input...)
	got, err := Build(input)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.FirstCapturedAt != "2026-08-01T12:00:00Z" || got.LastCapturedAt != "2026-08-02T12:00:00Z" {
		t.Fatalf("boundaries = %#v", got)
	}
	if input[0].CapturedAt != original[0].CapturedAt || input[1].CapturedAt != original[1].CapturedAt {
		t.Fatal("Build mutated caller input")
	}
}

func TestBuildSevenDaySpanStillNeedsOtherGates(t *testing.T) {
	got, err := Build([]observe.Sample{
		baselineSample("2026-08-01T00:00:00Z", "complete"),
		baselineSample("2026-08-08T00:00:00Z", "complete"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.ObservedSpanSeconds != int64((7*24*time.Hour)/time.Second) || got.ProgressPercent != 100 || got.Status != "collecting" {
		t.Fatalf("span status = %#v", got)
	}
	if len(got.Blockers) != 2 || got.Blockers[0] != "observed_days_insufficient" || got.Blockers[1] != "coverage_insufficient" {
		t.Fatalf("blockers = %#v", got.Blockers)
	}
}

func TestBuildDeduplicatesDaysAndThirtyMinuteIntervals(t *testing.T) {
	got, err := Build([]observe.Sample{
		baselineSample("2026-08-01T00:01:00Z", "complete"),
		baselineSample("2026-08-01T00:29:59Z", "complete"),
		baselineSample("2026-08-01T00:30:00Z", "complete"),
		baselineSample("2026-08-02T00:00:00Z", "complete"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.CoverageIntervals != 3 || got.ObservedDays != 2 {
		t.Fatalf("deduplicated coverage = %#v", got)
	}
}

func TestBuildReadyAtSevenDaysAndEightyFourIntervals(t *testing.T) {
	samples := make([]observe.Sample, 0, 84)
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 83; i++ {
		samples = append(samples, baselineSample(start.Add(time.Duration(i)*2*time.Hour).Format(time.RFC3339), "complete"))
	}
	samples = append(samples, baselineSample(start.Add(TargetSpan).Format(time.RFC3339), "complete"))
	got, err := Build(samples)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.Status != "coverage_ready" || len(got.Blockers) != 0 || got.CoverageIntervals != 84 || got.ObservedDays != 8 {
		t.Fatalf("ready status = %#v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"blockers":[]`) {
		t.Fatalf("ready blockers JSON = %s, want empty array", encoded)
	}
}

func TestBuildCountsDegradedSamplesAsValidSeparately(t *testing.T) {
	got, err := Build([]observe.Sample{
		baselineSample("2026-08-01T00:00:00Z", "degraded"),
		baselineSample("2026-08-01T00:30:00Z", "complete"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.ValidSamples != 2 || got.DegradedSamples != 1 || got.CompleteSamples != 1 || got.CoverageIntervals != 2 {
		t.Fatalf("quality counts = %#v", got)
	}
}

func TestBuildReportsLargestGapAndExactBoundaries(t *testing.T) {
	got, err := Build([]observe.Sample{
		baselineSample("2026-08-01T00:00:00Z", "complete"),
		baselineSample("2026-08-01T00:30:00Z", "complete"),
		baselineSample("2026-08-01T02:00:00Z", "complete"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.FirstCapturedAt != "2026-08-01T00:00:00Z" || got.LastCapturedAt != "2026-08-01T02:00:00Z" || got.ObservedSpanSeconds != 7200 || got.LargestGapSeconds != 5400 {
		t.Fatalf("timing = %#v", got)
	}
}

func TestBuildReportsExactSpanBeyondTimeDurationRange(t *testing.T) {
	got, err := Build([]observe.Sample{
		baselineSample("0000-01-01T00:00:00Z", "complete"),
		baselineSample("9999-12-31T23:59:59.999999999Z", "complete"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	const wantSeconds int64 = 315569519999
	if got.ObservedSpanSeconds != wantSeconds || got.LargestGapSeconds != wantSeconds {
		t.Fatalf("extreme timing = span %d gap %d, want %d", got.ObservedSpanSeconds, got.LargestGapSeconds, wantSeconds)
	}
}

func TestBuildFloorsPositiveSubsecondSpanAndGap(t *testing.T) {
	got, err := Build([]observe.Sample{
		baselineSample("2026-08-01T00:00:00.1Z", "complete"),
		baselineSample("2026-08-01T00:00:00.999999999Z", "complete"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.ObservedSpanSeconds != 0 || got.LargestGapSeconds != 0 {
		t.Fatalf("subsecond timing = span %d gap %d, want 0", got.ObservedSpanSeconds, got.LargestGapSeconds)
	}
}

func TestBuildProgressIsIntegerMonotonicAndCapped(t *testing.T) {
	before, err := Build([]observe.Sample{baselineSample("2026-08-01T00:00:00Z", "complete"), baselineSample("2026-08-07T23:59:59Z", "complete")})
	if err != nil {
		t.Fatalf("Build before: %v", err)
	}
	after, err := Build([]observe.Sample{baselineSample("2026-08-01T00:00:00Z", "complete"), baselineSample("2026-08-08T00:00:00Z", "complete"), baselineSample("2026-08-09T00:00:00Z", "complete")})
	if err != nil {
		t.Fatalf("Build after: %v", err)
	}
	if before.ProgressPercent < 99 || after.ProgressPercent != 100 || after.ProgressPercent < before.ProgressPercent {
		t.Fatalf("progress = %d then %d", before.ProgressPercent, after.ProgressPercent)
	}
}

func TestBuildOneSampleCollectsWithAllBlockers(t *testing.T) {
	got, err := Build([]observe.Sample{baselineSample("2026-08-01T12:00:00Z", "complete")})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.SchemaVersion != "stillmac.status.v1" || got.Status != "collecting" {
		t.Fatalf("status identity = %#v", got)
	}
	if !got.ReadOnly || got.RecommendationsEnabled {
		t.Fatalf("safety flags = %#v", got)
	}
	if got.ValidSamples != 1 || got.CoverageIntervals != 1 || got.ObservedDays != 1 {
		t.Fatalf("coverage = %#v", got)
	}
	if got.ObservedSpanSeconds != 0 || got.ProgressPercent != 0 || got.LargestGapSeconds != 0 {
		t.Fatalf("single sample timing = %#v", got)
	}
	if got.FirstCapturedAt != "2026-08-01T12:00:00Z" || got.LastCapturedAt != got.FirstCapturedAt {
		t.Fatalf("boundaries = %#v", got)
	}
	want := []string{"span_incomplete", "observed_days_insufficient", "coverage_insufficient"}
	if len(got.Blockers) != len(want) {
		t.Fatalf("blockers = %#v", got.Blockers)
	}
	for i := range want {
		if got.Blockers[i] != want[i] {
			t.Fatalf("blockers = %#v, want %#v", got.Blockers, want)
		}
	}
	if got.TargetSpanSeconds != int64((7*24*time.Hour)/time.Second) || got.ExpectedIntervalSeconds != 1800 || got.MinimumObservedDays != 7 || got.MinimumCoverageIntervals != 84 {
		t.Fatalf("contract constants = %#v", got)
	}
}
