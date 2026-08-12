package baseline

import (
	"errors"
	"sort"
	"time"

	"stillmac/internal/observe"
)

const (
	SchemaVersion            = "stillmac.status.v1"
	TargetSpan               = 7 * 24 * time.Hour
	ExpectedInterval         = 30 * time.Minute
	MinimumObservedDays      = 7
	MinimumCoverageIntervals = 84
)

var ErrInvalidSampleTimestamp = errors.New("invalid sample timestamp")

type Status struct {
	SchemaVersion            string   `json:"schema_version"`
	Status                   string   `json:"status"`
	ReadOnly                 bool     `json:"read_only"`
	RecommendationsEnabled   bool     `json:"recommendations_enabled"`
	TargetSpanSeconds        int64    `json:"target_span_seconds"`
	ExpectedIntervalSeconds  int64    `json:"expected_interval_seconds"`
	MinimumObservedDays      int      `json:"minimum_observed_days"`
	MinimumCoverageIntervals int      `json:"minimum_coverage_intervals"`
	ValidSamples             int      `json:"valid_samples"`
	CoverageIntervals        int      `json:"coverage_intervals"`
	ObservedDays             int      `json:"observed_days"`
	ObservedSpanSeconds      int64    `json:"observed_span_seconds"`
	ProgressPercent          int      `json:"progress_percent"`
	LargestGapSeconds        int64    `json:"largest_gap_seconds"`
	FirstCapturedAt          string   `json:"first_captured_at"`
	LastCapturedAt           string   `json:"last_captured_at"`
	CompleteSamples          int      `json:"complete_samples"`
	DegradedSamples          int      `json:"degraded_samples"`
	Blockers                 []string `json:"blockers"`
}

type timedSample struct {
	sample observe.Sample
	at     time.Time
}

func Build(samples []observe.Sample) (Status, error) {
	status := Status{
		SchemaVersion: SchemaVersion, Status: "collecting", ReadOnly: true,
		RecommendationsEnabled: false, TargetSpanSeconds: int64(TargetSpan / time.Second),
		ExpectedIntervalSeconds: int64(ExpectedInterval / time.Second), MinimumObservedDays: MinimumObservedDays,
		MinimumCoverageIntervals: MinimumCoverageIntervals,
	}
	ordered := make([]timedSample, len(samples))
	for i, sample := range samples {
		at, ok := canonicalUTC(sample.CapturedAt)
		if !ok {
			return status, ErrInvalidSampleTimestamp
		}
		ordered[i] = timedSample{sample: sample, at: at}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].at.Before(ordered[j].at) })
	status.ValidSamples = len(ordered)
	if len(ordered) == 0 {
		status.Blockers = allBlockers()
		return status, nil
	}
	status.FirstCapturedAt = ordered[0].sample.CapturedAt
	status.LastCapturedAt = ordered[len(ordered)-1].sample.CapturedAt
	for _, item := range ordered {
		if item.sample.Quality.Status == "degraded" {
			status.DegradedSamples++
		} else {
			status.CompleteSamples++
		}
	}
	status.ObservedSpanSeconds = elapsedWholeSeconds(ordered[0].at, ordered[len(ordered)-1].at)
	status.ProgressPercent = progress(status.ObservedSpanSeconds)
	status.CoverageIntervals = distinctIntervals(ordered)
	status.ObservedDays = distinctDays(ordered)
	status.LargestGapSeconds = largestGap(ordered)
	status.Blockers = blockers(status)
	if len(status.Blockers) == 0 {
		status.Status = "coverage_ready"
	}
	return status, nil
}

func canonicalUTC(value string) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || at.Location() != time.UTC || at.Format(time.RFC3339Nano) != value {
		return time.Time{}, false
	}
	return at, true
}

func elapsedWholeSeconds(earlier, later time.Time) int64 {
	if !later.After(earlier) {
		return 0
	}
	seconds := later.Unix() - earlier.Unix()
	if later.Nanosecond() < earlier.Nanosecond() {
		seconds--
	}
	return seconds
}

func progress(seconds int64) int {
	if seconds <= 0 {
		return 0
	}
	target := int64(TargetSpan / time.Second)
	if seconds >= target {
		return 100
	}
	return int((seconds/target)*100 + (seconds%target)*100/target)
}

func distinctIntervals(ordered []timedSample) int {
	seen := make(map[int64]struct{}, len(ordered))
	interval := int64(ExpectedInterval / time.Second)
	for _, item := range ordered {
		seconds := item.at.Unix()
		bucket := seconds / interval
		if seconds < 0 && seconds%interval != 0 {
			bucket--
		}
		seen[bucket] = struct{}{}
	}
	return len(seen)
}

func distinctDays(ordered []timedSample) int {
	seen := make(map[string]struct{}, len(ordered))
	for _, item := range ordered {
		seen[item.at.Format("2006-01-02")] = struct{}{}
	}
	return len(seen)
}

func largestGap(ordered []timedSample) int64 {
	var largest int64
	for i := 1; i < len(ordered); i++ {
		if gap := elapsedWholeSeconds(ordered[i-1].at, ordered[i].at); gap > largest {
			largest = gap
		}
	}
	return largest
}

func blockers(status Status) []string {
	result := make([]string, 0, 3)
	if status.ObservedSpanSeconds < status.TargetSpanSeconds {
		result = append(result, "span_incomplete")
	}
	if status.ObservedDays < status.MinimumObservedDays {
		result = append(result, "observed_days_insufficient")
	}
	if status.CoverageIntervals < status.MinimumCoverageIntervals {
		result = append(result, "coverage_insufficient")
	}
	return result
}

func allBlockers() []string {
	return []string{"span_incomplete", "observed_days_insufficient", "coverage_insufficient"}
}
