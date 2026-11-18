// Package model defines the governed data structures for docpresser/v0 packs,
// observations, run manifests, and verification reports.
package model

const (
	// Version is the governed schema contract identifier for Wave 1.
	Version = "docpresser/v0"
	// Generator identifies the deterministic SplitMix64 workload generator.
	Generator = "splitmix64-docpresser/v1"
	// StructcorpusPinnedCommit is the default pinned 3leaps/structcorpus commit for Wave 1.
	StructcorpusPinnedCommit = "b52fcd090d6879a0bd58ec5a5496c6a4de90504e"
)

// Pack represents pack.json in a bound corpus or generated synthetic pack.
type Pack struct {
	Version         string     `json:"version"`
	State           string     `json:"state"`
	PackKind        string     `json:"pack_kind"`
	FormatFamily    string     `json:"format_family"`
	Tier            string     `json:"tier"`
	Scenario        string     `json:"scenario,omitempty"`
	Generator       string     `json:"generator,omitempty"`
	Seed            string     `json:"seed,omitempty"`
	Scale           string     `json:"scale,omitempty"`
	CorpusCommit    string     `json:"corpus_commit,omitempty"`
	CorpusProfile   string     `json:"corpus_profile,omitempty"`
	SchemaSHA256    string     `json:"schema_sha256"`
	CaseCount       int        `json:"case_count"`
	TotalInputBytes int64      `json:"total_input_bytes"`
	Files           []PackFile `json:"files"`
}

// PackFile inventories a file inside a pack directory.
type PackFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	Records int    `json:"records"`
}

// PackCase represents one line in cases.ndjson inside a pack directory.
type PackCase struct {
	CaseID      string          `json:"case_id"`
	Format      string          `json:"format"`
	Axis        string          `json:"axis"`
	Profile     string          `json:"profile"`
	Operation   string          `json:"operation"`
	InputPath   string          `json:"input_path"`
	InputSHA256 string          `json:"input_sha256"`
	InputBytes  int64           `json:"input_bytes"`
	Expected    CaseExpectation `json:"expected"`
	Budgets     *CaseBudgets    `json:"budgets,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Description string          `json:"description,omitempty"`
}

// CaseExpectation defines the expected oracle outcome for a PackCase.
type CaseExpectation struct {
	Outcome               string   `json:"outcome"`
	OutputSHA256          string   `json:"output_sha256,omitempty"`
	CanonicalValue        string   `json:"canonical_value,omitempty"`
	ErrorPhase            string   `json:"error_phase,omitempty"`
	ErrorCode             string   `json:"error_code,omitempty"`
	YAML12CanonicalSHA256 string   `json:"yaml12_canonical_sha256,omitempty"`
	YAML11DivergentSHA256 string   `json:"yaml11_divergent_sha256,omitempty"`
	RequireNoDivergence   bool     `json:"require_no_divergence,omitempty"`
	MigrationCategory     string   `json:"migration_category,omitempty"`
	ExpectedHazards       []string `json:"expected_hazards,omitempty"`
}

// CaseBudgets defines per-case resource containment thresholds.
type CaseBudgets struct {
	MaxWallMs             int64   `json:"max_wall_ms,omitempty"`
	MaxRSSBytes           int64   `json:"max_rss_bytes,omitempty"`
	MaxOutputBytes        int64   `json:"max_output_bytes,omitempty"`
	MaxAmplificationRatio float64 `json:"max_amplification_ratio,omitempty"`
}

// Observation represents a single line in observations.ndjson.
type Observation struct {
	Version        string   `json:"version"`
	CaseID         string   `json:"case_id"`
	Subject        string   `json:"subject"`
	Role           string   `json:"role,omitempty"`
	Operation      string   `json:"operation"`
	InputSHA256    string   `json:"input_sha256"`
	InputBytes     int64    `json:"input_bytes"`
	Status         string   `json:"status"`
	OutputSHA256   string   `json:"output_sha256,omitempty"`
	OutputBytes    int64    `json:"output_bytes,omitempty"`
	DocumentCount  int      `json:"document_count,omitempty"`
	EventCount     int      `json:"event_count,omitempty"`
	CanonicalValue string   `json:"canonical_value,omitempty"`
	ErrorPhase     string   `json:"error_phase,omitempty"`
	ErrorCode      string   `json:"error_code,omitempty"`
	Signal         string   `json:"signal,omitempty"`
	DurationNs     int64    `json:"duration_ns"`
	ParseNs        int64    `json:"parse_ns,omitempty"`
	EmitNs         int64    `json:"emit_ns,omitempty"`
	PeakRSSBytes   int64    `json:"peak_rss_bytes"`
	ExitCode       int      `json:"exit_code"`
	HazardsFound   []string `json:"hazards_found,omitempty"`
}

// RunManifest represents run-manifest.json in an evidence bundle directory.
type RunManifest struct {
	Version            string            `json:"version"`
	DocpresserVersion  string            `json:"docpresser_version"`
	State              string            `json:"state"`
	RunID              string            `json:"run_id"`
	Profile            string            `json:"profile"`
	PackSHA256         string            `json:"pack_sha256"`
	SchemaSHA256       string            `json:"schema_sha256"`
	ObservationsSHA256 string            `json:"observations_sha256"`
	ObservationCount   int               `json:"observation_count"`
	StartedAt          string            `json:"started_at"`
	CompletedAt        string            `json:"completed_at"`
	Host               HostMetadata      `json:"host"`
	Subjects           []SubjectMetadata `json:"subjects"`
}

// HostMetadata records the platform and Go runtime executing the coordinator.
type HostMetadata struct {
	GOOS              string `json:"goos"`
	GOARCH            string `json:"goarch"`
	GoVersion         string `json:"go_version"`
	NumCPU            int    `json:"num_cpu"`
	CoordinatorSHA256 string `json:"coordinator_sha256"`
}

// SubjectMetadata describes a subject driver invoked during a run.
type SubjectMetadata struct {
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	SubjectVersion string   `json:"subject_version"`
	Language       string   `json:"language"`
	Engine         string   `json:"engine"`
	Formats        []string `json:"formats"`
	Operations     []string `json:"operations"`
	DriverSHA256   string   `json:"driver_sha256"`
}

// VerificationReport represents the structured output of the offline oracle.
type VerificationReport struct {
	Version             string             `json:"version"`
	RunID               string             `json:"run_id"`
	Profile             string             `json:"profile"`
	PackSHA256          string             `json:"pack_sha256"`
	ObservationsSHA256  string             `json:"observations_sha256"`
	Verdict             string             `json:"verdict"`
	Correctness         CorrectnessSummary `json:"correctness"`
	Containment         ContainmentSummary `json:"containment"`
	MigrationDivergence MigrationSummary   `json:"migration_divergence"`
	Telemetry           []SubjectTelemetry `json:"telemetry"`
	Findings            []Finding          `json:"findings"`
}

// CorrectnessSummary summarizes Axis 1 (Specification & Canonical Correctness).
type CorrectnessSummary struct {
	Verdict      string `json:"verdict"`
	Passed       int    `json:"passed"`
	Failed       int    `json:"failed"`
	Unsupported  int    `json:"unsupported"`
	Inconclusive int    `json:"inconclusive"`
}

// ContainmentSummary summarizes Axis 2 (Resource Containment & Failure Mode).
type ContainmentSummary struct {
	Verdict               string `json:"verdict"`
	RefusedBounded        int    `json:"refused_bounded"`
	OOMKilled             int    `json:"oom_killed"`
	CrashSignal           int    `json:"crash_signal"`
	TimeoutExceeded       int    `json:"timeout_exceeded"`
	AmplificationExceeded int    `json:"amplification_exceeded"`
}

// MigrationSummary summarizes Axis 3 (Cross-Edition Migration Divergence).
type MigrationSummary struct {
	TotalFindings int                `json:"total_findings"`
	ByCategory    map[string]int     `json:"by_category"`
	Findings      []MigrationFinding `json:"findings"`
}

// MigrationFinding records how a subject handled a YAML 1.1 vs 1.2 construct.
type MigrationFinding struct {
	CaseID          string `json:"case_id"`
	Category        string `json:"category"`
	TargetSubject   string `json:"target_subject"`
	BaselineSubject string `json:"baseline_subject"`
	Summary         string `json:"summary"`
}

// SubjectTelemetry summarizes Axis 4 (Comparative Telemetry) for one subject.
type SubjectTelemetry struct {
	Subject               string  `json:"subject"`
	Role                  string  `json:"role"`
	Cases                 int     `json:"cases"`
	TotalInputBytes       int64   `json:"total_input_bytes"`
	TotalDurationNs       int64   `json:"total_duration_ns"`
	ThroughputMiBPerS     float64 `json:"throughput_mib_per_s"`
	DocsPerS              float64 `json:"docs_per_s"`
	P50Ns                 int64   `json:"p50_ns"`
	P95Ns                 int64   `json:"p95_ns"`
	P99Ns                 int64   `json:"p99_ns"`
	PeakRSSBytes          int64   `json:"peak_rss_bytes"`
	MaxAmplificationRatio float64 `json:"max_amplification_ratio"`
}

// Finding represents a specific verification failure or discrepancy.
type Finding struct {
	Axis    string `json:"axis"`
	CaseID  string `json:"case_id,omitempty"`
	Subject string `json:"subject,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
