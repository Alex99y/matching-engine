package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/alex99y/matching-engine/loadtest/internal/correlate"
)

// Metric is one named latency measurement (e.g. "ack", "match", "cancel_round_trip") plus the
// orders that never produced a sample for it, broken down by why.
type Metric struct {
	Name     string
	Samples  []correlate.Sample
	Dead     []correlate.DeadOrder // reached a terminal status that could never satisfy this metric
	Timeouts []correlate.DeadOrder // still pending when the run's sweep deadline passed
}

func (m Metric) durations() []time.Duration {
	out := make([]time.Duration, len(m.Samples))
	for i, s := range m.Samples {
		out[i] = s.Latency
	}
	return out
}

// Run is everything measured during one cmd invocation, ready to print and persist.
type Run struct {
	TestName  string
	Level     int
	LevelName string
	Market    string
	StartedAt time.Time
	Duration  time.Duration

	TargetSpamRate   int
	AchievedSpamRate float64
	StreamReconnects int64

	Metrics []Metric
}

type summaryMetric struct {
	Name         string      `json:"name"`
	Percentiles  Percentiles `json:"percentiles"`
	SampleCount  int         `json:"sample_count"`
	DeadCount    int         `json:"dead_count"`
	TimeoutCount int         `json:"timeout_count"`
}

type summaryDoc struct {
	TestName         string          `json:"test_name"`
	Level            int             `json:"level"`
	LevelName        string          `json:"level_name"`
	Market           string          `json:"market"`
	StartedAt        time.Time       `json:"started_at"`
	Duration         string          `json:"duration"`
	TargetSpamRate   int             `json:"target_spam_rate_ops_per_sec"`
	AchievedSpamRate float64         `json:"achieved_spam_rate_ops_per_sec"`
	StreamReconnects int64           `json:"stream_reconnects"`
	Metrics          []summaryMetric `json:"metrics"`
}

func (r Run) summary() summaryDoc {
	doc := summaryDoc{
		TestName:         r.TestName,
		Level:            r.Level,
		LevelName:        r.LevelName,
		Market:           r.Market,
		StartedAt:        r.StartedAt,
		Duration:         r.Duration.String(),
		TargetSpamRate:   r.TargetSpamRate,
		AchievedSpamRate: r.AchievedSpamRate,
		StreamReconnects: r.StreamReconnects,
	}
	for _, m := range r.Metrics {
		doc.Metrics = append(doc.Metrics, summaryMetric{
			Name:         m.Name,
			Percentiles:  computePercentiles(m.durations()),
			SampleCount:  len(m.Samples),
			DeadCount:    len(m.Dead),
			TimeoutCount: len(m.Timeouts),
		})
	}
	return doc
}

// PrintSummary writes a human-readable table to w.
func (r Run) PrintSummary(w io.Writer) {
	fmt.Fprintf(w, "\n=== %s — level %d (%s), market %s ===\n", r.TestName, r.Level, r.LevelName, r.Market)
	fmt.Fprintf(w, "duration: %s   spam target: %d op/s   spam achieved: %.1f op/s   stream reconnects: %d\n\n",
		r.Duration, r.TargetSpamRate, r.AchievedSpamRate, r.StreamReconnects)

	fmt.Fprintf(w, "%-22s %6s %6s %10s %10s %10s %10s %10s %10s %10s\n",
		"metric", "n", "dead", "timeout", "min", "p50", "p90", "p95", "p99", "max")
	for _, m := range r.Metrics {
		p := computePercentiles(m.durations())
		fmt.Fprintf(w, "%-22s %6d %6d %10d %10s %10s %10s %10s %10s %10s\n",
			m.Name, p.Count, len(m.Dead), len(m.Timeouts),
			fmtMs(p.Min), fmtMs(p.P50), fmtMs(p.P90), fmtMs(p.P95), fmtMs(p.P99), fmtMs(p.Max))
	}
	fmt.Fprintln(w)
}

func fmtMs(d time.Duration) string {
	return strconv.FormatFloat(msOf(d), 'f', 2, 64) + "ms"
}

// Persist writes summary.json and samples.csv into a timestamped subdirectory of outputDir,
// creating it as needed, and returns that directory's path.
func (r Run) Persist(outputDir string) (string, error) {
	dir := filepath.Join(outputDir, fmt.Sprintf("%s-level%d-%s", r.TestName, r.Level, r.StartedAt.Format("20060102T150405Z0700")))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	if err := r.writeJSON(filepath.Join(dir, "summary.json")); err != nil {
		return "", err
	}
	if err := r.writeCSV(filepath.Join(dir, "samples.csv")); err != nil {
		return "", err
	}
	return dir, nil
}

func (r Run) writeJSON(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r.summary()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (r Run) writeCSV(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"metric", "order_id", "sent_at", "latency_ms", "event_status"}); err != nil {
		return fmt.Errorf("write %s header: %w", path, err)
	}
	for _, m := range r.Metrics {
		for _, s := range m.Samples {
			row := []string{
				m.Name,
				s.OrderID.String(),
				s.SentAt.Format(time.RFC3339Nano),
				strconv.FormatFloat(msOf(s.Latency), 'f', 3, 64),
				s.Event.Status,
			}
			if err := w.Write(row); err != nil {
				return fmt.Errorf("write %s row: %w", path, err)
			}
		}
	}
	return nil
}
