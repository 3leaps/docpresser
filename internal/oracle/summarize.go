package oracle

import (
	"fmt"
	"sort"
	"strings"

	"github.com/3leaps/docpresser/internal/model"
)

// SummarizeMarkdown renders a VerificationReport as a human-readable Markdown scorecard.
func SummarizeMarkdown(r model.VerificationReport) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "# Docpresser Qualification Report — `%s`\n\n", r.RunID)
	_, _ = fmt.Fprintf(&b, "- **Verdict**: `%s`\n", r.Verdict)
	_, _ = fmt.Fprintf(&b, "- **Profile**: `%s`\n", r.Profile)
	_, _ = fmt.Fprintf(&b, "- **Pack SHA-256**: `%s`\n", r.PackSHA256)
	_, _ = fmt.Fprintf(&b, "- **Observations SHA-256**: `%s`\n\n", r.ObservationsSHA256)

	b.WriteString("## 1. Correctness Summary\n\n")
	b.WriteString("| Verdict | Passed | Failed | Unsupported | Inconclusive |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
	_, _ = fmt.Fprintf(&b,
		"| `%s` | %d | %d | %d | %d |\n\n",
		r.Correctness.Verdict,
		r.Correctness.Passed,
		r.Correctness.Failed,
		r.Correctness.Unsupported,
		r.Correctness.Inconclusive,
	)

	b.WriteString("## 2. Containment Summary\n\n")
	b.WriteString("| Verdict | Refused Bounded | OOM Killed | Crash Signal | Timeout Exceeded | Amplification Exceeded |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
	_, _ = fmt.Fprintf(&b,
		"| `%s` | %d | %d | %d | %d | %d |\n\n",
		r.Containment.Verdict,
		r.Containment.RefusedBounded,
		r.Containment.OOMKilled,
		r.Containment.CrashSignal,
		r.Containment.TimeoutExceeded,
		r.Containment.AmplificationExceeded,
	)

	b.WriteString("## 3. Migration Divergence\n\n")
	_, _ = fmt.Fprintf(&b, "- **Total Divergences**: %d\n", r.MigrationDivergence.TotalFindings)
	if len(r.MigrationDivergence.ByCategory) > 0 {
		cats := make([]string, 0, len(r.MigrationDivergence.ByCategory))
		for k := range r.MigrationDivergence.ByCategory {
			cats = append(cats, k)
		}
		sort.Strings(cats)
		for _, k := range cats {
			_, _ = fmt.Fprintf(&b, "  - `%s`: %d\n", k, r.MigrationDivergence.ByCategory[k])
		}
	}
	b.WriteString("\n")
	if len(r.MigrationDivergence.Findings) > 0 {
		b.WriteString("| Case ID | Category | Target | Baseline | Summary |\n")
		b.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, mf := range r.MigrationDivergence.Findings {
			_, _ = fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | `%s` | %s |\n",
				mf.CaseID, mf.Category, mf.TargetSubject, mf.BaselineSubject, mf.Summary)
		}
		b.WriteString("\n")
	}

	b.WriteString("## 4. Comparative Telemetry\n\n")
	if len(r.Telemetry) == 0 {
		b.WriteString("_No subject telemetry recorded._\n\n")
	} else {
		b.WriteString("| Subject | Role | Cases | Throughput (MiB/s) | Docs/s | p50 (ns) | p95 (ns) | p99 (ns) | Peak RSS (B) | Max Amp Ratio |\n")
		b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, t := range r.Telemetry {
			_, _ = fmt.Fprintf(&b,
				"| `%s` | `%s` | %d | %.3f | %.3f | %d | %d | %d | %d | %.3f |\n",
				t.Subject, t.Role, t.Cases, t.ThroughputMiBPerS, t.DocsPerS,
				t.P50Ns, t.P95Ns, t.P99Ns, t.PeakRSSBytes, t.MaxAmplificationRatio,
			)
		}
		b.WriteString("\n")
	}

	if len(r.Findings) > 0 {
		b.WriteString("## Findings\n\n")
		b.WriteString("| Axis | Code | Subject | Case ID | Message |\n")
		b.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, f := range r.Findings {
			_, _ = fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | `%s` | %s |\n",
				f.Axis, f.Code, f.Subject, f.CaseID, f.Message)
		}
	}
	return b.String()
}
