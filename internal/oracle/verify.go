// Package oracle implements the zero-trust offline verifier for docpresser/v0
// evidence bundles against their bound or generated packs.
package oracle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/3leaps/docpresser/internal/model"
	v0 "github.com/3leaps/docpresser/schemas/docpresser/v0"
)

// Verify evaluates an evidence bundle directory (runDir) against its pack directory (packDir).
// Both paths are required; the oracle never trusts self-reported digests without checking the pack.
func Verify(ctx context.Context, runDir, packDir string) (*model.VerificationReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(runDir) == "" {
		return nil, fmt.Errorf("--run directory is required")
	}
	if strings.TrimSpace(packDir) == "" {
		return nil, fmt.Errorf("--pack directory is required")
	}

	packRoot, err := os.OpenRoot(packDir)
	if err != nil {
		return nil, fmt.Errorf("open pack directory: %w", err)
	}
	defer func() { _ = packRoot.Close() }()

	runRoot, err := os.OpenRoot(runDir)
	if err != nil {
		return nil, fmt.Errorf("open run directory: %w", err)
	}
	defer func() { _ = runRoot.Close() }()

	report := &model.VerificationReport{
		Version:            model.Version,
		RunID:              "unknown",
		Profile:            "unknown",
		PackSHA256:         strings.Repeat("0", 64),
		ObservationsSHA256: strings.Repeat("0", 64),
		Verdict:            "pass",
		Correctness: model.CorrectnessSummary{
			Verdict: "pass",
		},
		Containment: model.ContainmentSummary{
			Verdict: "not_applicable",
		},
		MigrationDivergence: model.MigrationSummary{
			ByCategory: make(map[string]int),
			Findings:   make([]model.MigrationFinding, 0),
		},
		Telemetry: make([]model.SubjectTelemetry, 0),
		Findings:  make([]model.Finding, 0),
	}

	addFinding := func(axis, caseID, subject, code, msg string) {
		report.Findings = append(report.Findings, model.Finding{
			Axis:    axis,
			CaseID:  caseID,
			Subject: subject,
			Code:    code,
			Message: msg,
		})
	}

	packMeta, actualPackSHA, cases, packOK := verifyPackRoot(packRoot, report, addFinding)
	if actualPackSHA != "" {
		report.PackSHA256 = actualPackSHA
	}
	if packMeta.Tier != "" {
		report.Profile = packMeta.Tier
	}

	manifest, manifestOK := verifyManifest(runRoot, actualPackSHA, report, addFinding)
	if manifest.RunID != "" {
		report.RunID = manifest.RunID
	}
	if manifest.Profile != "" {
		report.Profile = manifest.Profile
	}
	if len(manifest.ObservationsSHA256) == 64 {
		report.ObservationsSHA256 = manifest.ObservationsSHA256
	}

	observations, obsOK := verifyObservationsStream(runRoot, manifest, manifestOK, addFinding)
	if !packOK || !manifestOK || !obsOK {
		report.Verdict = "fail"
		if report.Correctness.Failed == 0 {
			report.Correctness.Failed = 1
		}
		report.Correctness.Verdict = "fail"
		return finalizeReport(report)
	}

	evaluateObservations(cases, manifest, observations, report, addFinding)
	return finalizeReport(report)
}

func verifyPackRoot(
	packRoot *os.Root,
	report *model.VerificationReport,
	addFinding func(axis, caseID, subject, code, msg string),
) (model.Pack, string, []model.PackCase, bool) {
	packBytes, err := readRegularFile(packRoot, "pack.json")
	if err != nil {
		addFinding("pack", "", "", "missing_pack_json", fmt.Sprintf("read pack.json: %v", err))
		return model.Pack{}, "", nil, false
	}
	actualPackSHA := sha256Hex(packBytes)
	report.PackSHA256 = actualPackSHA

	var meta model.Pack
	if err := v0.Decode(packBytes, "pack.schema.json", &meta); err != nil {
		addFinding("pack", "", "", "invalid_pack_schema", err.Error())
		return meta, actualPackSHA, nil, false
	}
	if meta.SchemaSHA256 != v0.SchemaSHA256() {
		addFinding("pack", "", "", "schema_sha256_mismatch", "pack.json schema_sha256 does not match verifier embedded schemas")
		return meta, actualPackSHA, nil, false
	}

	fileMap := make(map[string]model.PackFile, len(meta.Files))
	var sumInputBytes int64
	for _, f := range meta.Files {
		if _, dup := fileMap[f.Path]; dup {
			addFinding("pack", "", "", "duplicate_pack_file", fmt.Sprintf("duplicate file %q in pack.json", f.Path))
			return meta, actualPackSHA, nil, false
		}
		raw, readErr := readRegularFile(packRoot, f.Path)
		if readErr != nil {
			addFinding("pack", "", "", "missing_pack_file", fmt.Sprintf("missing pack file %q: %v", f.Path, readErr))
			return meta, actualPackSHA, nil, false
		}
		if int64(len(raw)) != f.Bytes || sha256Hex(raw) != f.SHA256 {
			addFinding("pack", "", "", "pack_file_tampered", fmt.Sprintf("pack file %q digest or byte length mismatch", f.Path))
			return meta, actualPackSHA, nil, false
		}
		if f.Path != "cases.ndjson" {
			sumInputBytes += f.Bytes
		}
		fileMap[f.Path] = f
	}

	casesEntry, ok := fileMap["cases.ndjson"]
	if !ok {
		addFinding("pack", "", "", "missing_cases_ndjson", "pack.json files inventory does not list cases.ndjson")
		return meta, actualPackSHA, nil, false
	}
	if sumInputBytes != meta.TotalInputBytes {
		addFinding("pack", "", "", "total_input_bytes_mismatch", "pack.json total_input_bytes does not match listed input files")
		return meta, actualPackSHA, nil, false
	}

	casesRaw, err := readRegularFile(packRoot, "cases.ndjson")
	if err != nil {
		addFinding("pack", "", "", "missing_cases_ndjson", err.Error())
		return meta, actualPackSHA, nil, false
	}
	cases, err := parseCases(casesRaw)
	if err != nil {
		addFinding("pack", "", "", "invalid_cases_ndjson", err.Error())
		return meta, actualPackSHA, nil, false
	}
	if len(cases) != meta.CaseCount || len(cases) != casesEntry.Records {
		addFinding("pack", "", "", "case_count_mismatch", fmt.Sprintf("cases.ndjson has %d cases, pack.json expects %d", len(cases), meta.CaseCount))
		return meta, actualPackSHA, nil, false
	}
	for _, c := range cases {
		f, exists := fileMap[c.InputPath]
		if !exists {
			addFinding("pack", c.CaseID, "", "unlisted_case_input", fmt.Sprintf("case %q input_path %q is not listed in pack.json", c.CaseID, c.InputPath))
			return meta, actualPackSHA, nil, false
		}
		if f.SHA256 != c.InputSHA256 || f.Bytes != c.InputBytes {
			addFinding("pack", c.CaseID, "", "case_input_digest_mismatch", fmt.Sprintf("case %q input metadata does not match pack.json file entry", c.CaseID))
			return meta, actualPackSHA, nil, false
		}
	}
	return meta, actualPackSHA, cases, true
}

func verifyManifest(
	runRoot *os.Root,
	actualPackSHA string,
	report *model.VerificationReport,
	addFinding func(axis, caseID, subject, code, msg string),
) (model.RunManifest, bool) {
	manifestBytes, err := readRegularFile(runRoot, "run-manifest.json")
	if err != nil {
		addFinding("manifest", "", "", "missing_run_manifest", fmt.Sprintf("read run-manifest.json: %v", err))
		return model.RunManifest{}, false
	}
	var manifest model.RunManifest
	if err := v0.Decode(manifestBytes, "run-manifest.schema.json", &manifest); err != nil {
		addFinding("manifest", "", "", "invalid_run_manifest", err.Error())
		return manifest, false
	}
	ok := true
	if manifest.State != "complete" {
		addFinding("manifest", "", "", "incomplete_run", fmt.Sprintf("run-manifest.json state is %q, want \"complete\"", manifest.State))
		ok = false
	}
	if manifest.SchemaSHA256 != v0.SchemaSHA256() {
		addFinding("manifest", "", "", "schema_sha256_mismatch", "run-manifest.json schema_sha256 does not match verifier embedded schemas")
		ok = false
	}
	if actualPackSHA != "" && manifest.PackSHA256 != actualPackSHA {
		addFinding("manifest", "", "", "pack_sha256_mismatch", fmt.Sprintf("run-manifest.json pack_sha256 %s does not match pack.json %s", manifest.PackSHA256, actualPackSHA))
		ok = false
	}
	return manifest, ok
}

func verifyObservationsStream(
	runRoot *os.Root,
	manifest model.RunManifest,
	manifestDecoded bool,
	addFinding func(axis, caseID, subject, code, msg string),
) ([]model.Observation, bool) {
	obsBytes, err := readRegularFile(runRoot, "observations.ndjson")
	if err != nil {
		addFinding("manifest", "", "", "missing_observations", fmt.Sprintf("read observations.ndjson: %v", err))
		return nil, false
	}
	actualObsSHA := sha256Hex(obsBytes)
	ok := true
	if manifestDecoded && len(manifest.ObservationsSHA256) == 64 && actualObsSHA != manifest.ObservationsSHA256 {
		addFinding("manifest", "", "", "observations_sha256_mismatch", fmt.Sprintf("observations.ndjson sha256 %s does not match run-manifest.json %s", actualObsSHA, manifest.ObservationsSHA256))
		ok = false
	}

	trimmed := bytes.TrimRight(obsBytes, "\r\n")
	if len(trimmed) == 0 {
		if manifestDecoded && manifest.ObservationCount != 0 {
			addFinding("manifest", "", "", "observation_count_mismatch", fmt.Sprintf("observations.ndjson is empty, run-manifest.json expects %d", manifest.ObservationCount))
			return nil, false
		}
		return nil, ok
	}
	lines := bytes.Split(trimmed, []byte{'\n'})
	obsList := make([]model.Observation, 0, len(lines))
	for i, line := range lines {
		var obs model.Observation
		if err := v0.Decode(line, "observation.schema.json", &obs); err != nil {
			addFinding("manifest", "", "", "invalid_observation_schema", fmt.Sprintf("line %d: %v", i+1, err))
			return nil, false
		}
		obsList = append(obsList, obs)
	}
	if manifestDecoded && len(obsList) != manifest.ObservationCount {
		addFinding("manifest", "", "", "observation_count_mismatch", fmt.Sprintf("observations.ndjson has %d records, run-manifest.json expects %d", len(obsList), manifest.ObservationCount))
		ok = false
	}
	return obsList, ok
}

func evaluateObservations(
	cases []model.PackCase,
	manifest model.RunManifest,
	observations []model.Observation,
	report *model.VerificationReport,
	addFinding func(axis, caseID, subject, code, msg string),
) {
	caseMap := make(map[string]model.PackCase, len(cases))
	hasContainmentCases := false
	for _, c := range cases {
		caseMap[c.CaseID] = c
		if c.Axis == "containment" {
			hasContainmentCases = true
		}
	}

	subjectMeta := make(map[string]model.SubjectMetadata, len(manifest.Subjects))
	for _, s := range manifest.Subjects {
		subjectMeta[s.Name] = s
	}

	type subjCaseKey struct {
		subject string
		caseID  string
	}
	observed := make(map[subjCaseKey]model.Observation, len(observations))
	bySubject := make(map[string][]model.Observation, len(manifest.Subjects))

	for _, obs := range observations {
		if _, knownSubj := subjectMeta[obs.Subject]; !knownSubj {
			addFinding("manifest", obs.CaseID, obs.Subject, "undeclared_subject", fmt.Sprintf("observation subject %q is not listed in run-manifest.json", obs.Subject))
			report.Correctness.Failed++
			continue
		}
		pc, knownCase := caseMap[obs.CaseID]
		if !knownCase {
			addFinding("pack", obs.CaseID, obs.Subject, "spurious_case", fmt.Sprintf("observation references case %q not present in pack", obs.CaseID))
			report.Correctness.Failed++
			continue
		}
		key := subjCaseKey{subject: obs.Subject, caseID: obs.CaseID}
		if _, dup := observed[key]; dup {
			addFinding("manifest", obs.CaseID, obs.Subject, "duplicate_observation", fmt.Sprintf("duplicate observation for subject %q case %q", obs.Subject, obs.CaseID))
			report.Correctness.Failed++
			continue
		}
		if obs.InputSHA256 != pc.InputSHA256 || obs.InputBytes != pc.InputBytes {
			addFinding("pack", obs.CaseID, obs.Subject, "input_mismatch", fmt.Sprintf("observation input digest/bytes for %q do not match pack case", obs.CaseID))
			report.Correctness.Failed++
			continue
		}
		observed[key] = obs
		bySubject[obs.Subject] = append(bySubject[obs.Subject], obs)
	}

	for _, s := range manifest.Subjects {
		for _, c := range cases {
			key := subjCaseKey{subject: s.Name, caseID: c.CaseID}
			obs, exists := observed[key]
			if !exists {
				addFinding("pack", c.CaseID, s.Name, "missing_case", fmt.Sprintf("subject %q did not record an observation for case %q", s.Name, c.CaseID))
				report.Correctness.Failed++
				continue
			}
			evaluateSingleCase(c, s, obs, report, &hasContainmentCases, addFinding)
		}
		report.Telemetry = append(report.Telemetry, computeSubjectTelemetry(s, bySubject[s.Name]))
	}

	updateAxisVerdicts(report, hasContainmentCases)
}

func evaluateSingleCase(
	pc model.PackCase,
	subj model.SubjectMetadata,
	obs model.Observation,
	report *model.VerificationReport,
	hasContainment *bool,
	addFinding func(axis, caseID, subject, code, msg string),
) {
	// Check catastrophic process/containment statuses across all axes
	switch obs.Status {
	case "crash_signal":
		*hasContainment = true
		report.Containment.CrashSignal++
		report.Correctness.Failed++
		addFinding("containment", pc.CaseID, subj.Name, "crash_signal", fmt.Sprintf("subject %q terminated with signal %q on case %q", subj.Name, obs.Signal, pc.CaseID))
		return
	case "oom_killed":
		*hasContainment = true
		report.Containment.OOMKilled++
		report.Correctness.Failed++
		addFinding("containment", pc.CaseID, subj.Name, "oom_killed", fmt.Sprintf("subject %q exceeded memory ceiling on case %q", subj.Name, pc.CaseID))
		return
	case "timeout_exceeded":
		*hasContainment = true
		report.Containment.TimeoutExceeded++
		report.Correctness.Failed++
		addFinding("containment", pc.CaseID, subj.Name, "timeout_exceeded", fmt.Sprintf("subject %q exceeded wall-clock timeout on case %q", subj.Name, pc.CaseID))
		return
	case "protocol_error":
		*hasContainment = true
		report.Containment.CrashSignal++
		report.Correctness.Failed++
		addFinding("containment", pc.CaseID, subj.Name, "protocol_error", fmt.Sprintf("subject %q produced malformed driver output on case %q", subj.Name, pc.CaseID))
		return
	}

	// Check explicit resource budgets when configured
	budgetBreached := false
	if pc.Budgets != nil {
		*hasContainment = true
		if pc.Budgets.MaxWallMs > 0 && obs.DurationNs > pc.Budgets.MaxWallMs*1_000_000 {
			budgetBreached = true
			report.Containment.TimeoutExceeded++
			addFinding("containment", pc.CaseID, subj.Name, "wall_budget_exceeded", fmt.Sprintf("duration %d ns exceeded budget %d ms", obs.DurationNs, pc.Budgets.MaxWallMs))
		}
		if pc.Budgets.MaxRSSBytes > 0 && obs.PeakRSSBytes > pc.Budgets.MaxRSSBytes {
			budgetBreached = true
			report.Containment.AmplificationExceeded++
			addFinding("containment", pc.CaseID, subj.Name, "rss_budget_exceeded", fmt.Sprintf("peak_rss_bytes %d exceeded budget %d", obs.PeakRSSBytes, pc.Budgets.MaxRSSBytes))
		}
		if pc.Budgets.MaxOutputBytes > 0 && obs.OutputBytes > pc.Budgets.MaxOutputBytes {
			budgetBreached = true
			report.Containment.AmplificationExceeded++
			addFinding("containment", pc.CaseID, subj.Name, "output_budget_exceeded", fmt.Sprintf("output_bytes %d exceeded budget %d", obs.OutputBytes, pc.Budgets.MaxOutputBytes))
		}
		if pc.Budgets.MaxAmplificationRatio > 0 {
			ratio := amplificationRatio(obs.InputBytes, obs.OutputBytes)
			if ratio > pc.Budgets.MaxAmplificationRatio {
				budgetBreached = true
				report.Containment.AmplificationExceeded++
				addFinding("containment", pc.CaseID, subj.Name, "amplification_budget_exceeded", fmt.Sprintf("amplification ratio %.2f exceeded budget %.2f", ratio, pc.Budgets.MaxAmplificationRatio))
			}
		}
	}
	if budgetBreached {
		report.Correctness.Failed++
		return
	}

	switch pc.Axis {
	case "containment":
		*hasContainment = true
		switch obs.Status {
		case "refused_bounded", "error":
			report.Containment.RefusedBounded++
			report.Correctness.Passed++
		case "ok":
			report.Containment.AmplificationExceeded++
			report.Correctness.Failed++
			addFinding("containment", pc.CaseID, subj.Name, "unbounded_accept", fmt.Sprintf("subject %q accepted adversarial containment case %q without refusal", subj.Name, pc.CaseID))
		case "unsupported":
			report.Correctness.Unsupported++
		default:
			report.Correctness.Inconclusive++
		}

	case "migration_divergence":
		evaluateMigrationCase(pc, subj, obs, report, addFinding)

	default: // correctness
		evaluateCorrectnessCase(pc, subj, obs, report, addFinding)
	}
}

func evaluateCorrectnessCase(
	pc model.PackCase,
	subj model.SubjectMetadata,
	obs model.Observation,
	report *model.VerificationReport,
	addFinding func(axis, caseID, subject, code, msg string),
) {
	if obs.Status == "unsupported" {
		report.Correctness.Unsupported++
		return
	}
	switch pc.Expected.Outcome {
	case "accept":
		if obs.Status != "ok" {
			report.Correctness.Failed++
			addFinding("correctness", pc.CaseID, subj.Name, "unexpected_error", fmt.Sprintf("subject %q rejected valid case %q (status=%s, error_code=%s)", subj.Name, pc.CaseID, obs.Status, obs.ErrorCode))
			return
		}
		if pc.Expected.OutputSHA256 != "" && obs.OutputSHA256 != pc.Expected.OutputSHA256 {
			report.Correctness.Failed++
			addFinding("correctness", pc.CaseID, subj.Name, "digest_mismatch", fmt.Sprintf("subject %q output_sha256 %s does not match expected %s", subj.Name, obs.OutputSHA256, pc.Expected.OutputSHA256))
			return
		}
		report.Correctness.Passed++

	case "reject", "refused_bounded":
		if obs.Status == "error" || obs.Status == "refused_bounded" {
			if pc.Expected.ErrorPhase != "" && obs.ErrorPhase != "" && obs.ErrorPhase != pc.Expected.ErrorPhase {
				report.Correctness.Failed++
				addFinding("correctness", pc.CaseID, subj.Name, "error_phase_mismatch", fmt.Sprintf("subject %q error_phase %q does not match expected %q", subj.Name, obs.ErrorPhase, pc.Expected.ErrorPhase))
				return
			}
			report.Correctness.Passed++
			return
		}
		report.Correctness.Failed++
		addFinding("correctness", pc.CaseID, subj.Name, "unexpected_accept", fmt.Sprintf("subject %q accepted negative case %q", subj.Name, pc.CaseID))
	}
}

func evaluateMigrationCase(
	pc model.PackCase,
	subj model.SubjectMetadata,
	obs model.Observation,
	report *model.VerificationReport,
	addFinding func(axis, caseID, subject, code, msg string),
) {
	if obs.Status == "unsupported" {
		report.Correctness.Unsupported++
		return
	}
	if obs.Status != "ok" {
		report.Correctness.Failed++
		addFinding("migration_divergence", pc.CaseID, subj.Name, "unexpected_error", fmt.Sprintf("subject %q failed migration case %q with status %s", subj.Name, pc.CaseID, obs.Status))
		return
	}

	expectedYAML12 := pc.Expected.YAML12CanonicalSHA256
	if expectedYAML12 == "" {
		expectedYAML12 = pc.Expected.OutputSHA256
	}
	if expectedYAML12 == "" || obs.OutputSHA256 == expectedYAML12 {
		report.Correctness.Passed++
		return
	}

	category := pc.Expected.MigrationCategory
	if category == "" {
		category = "yaml11_divergence"
	}
	report.MigrationDivergence.TotalFindings++
	report.MigrationDivergence.ByCategory[category]++
	report.MigrationDivergence.Findings = append(report.MigrationDivergence.Findings, model.MigrationFinding{
		CaseID:          pc.CaseID,
		Category:        category,
		TargetSubject:   "yaml12-core",
		BaselineSubject: subj.Name,
		Summary:         fmt.Sprintf("subject %q produced digest %s (YAML 1.2 Core expects %s)", subj.Name, obs.OutputSHA256, expectedYAML12),
	})

	if pc.Expected.RequireNoDivergence || subj.Role == "target" {
		report.Correctness.Failed++
		addFinding("migration_divergence", pc.CaseID, subj.Name, "unexpected_migration_divergence", fmt.Sprintf("subject %q (role=%s) diverged from YAML 1.2 Core on case %q", subj.Name, subj.Role, pc.CaseID))
	} else {
		report.Correctness.Passed++
	}
}

func updateAxisVerdicts(report *model.VerificationReport, hasContainment bool) {
	switch {
	case report.Correctness.Failed > 0:
		report.Correctness.Verdict = "fail"
	case report.Correctness.Passed > 0:
		report.Correctness.Verdict = "pass"
	case report.Correctness.Unsupported > 0:
		report.Correctness.Verdict = "unsupported"
	default:
		report.Correctness.Verdict = "inconclusive"
	}

	switch {
	case report.Containment.OOMKilled > 0:
		report.Containment.Verdict = "oom_killed"
	case report.Containment.CrashSignal > 0:
		report.Containment.Verdict = "crash_signal"
	case report.Containment.TimeoutExceeded > 0:
		report.Containment.Verdict = "timeout_exceeded"
	case report.Containment.AmplificationExceeded > 0:
		report.Containment.Verdict = "amplification_exceeded"
	case hasContainment || report.Containment.RefusedBounded > 0:
		report.Containment.Verdict = "refused_bounded"
	default:
		report.Containment.Verdict = "not_applicable"
	}

	if len(report.Findings) > 0 || report.Correctness.Verdict == "fail" {
		report.Verdict = "fail"
	} else if report.Correctness.Verdict == "inconclusive" {
		report.Verdict = "inconclusive"
	} else {
		report.Verdict = "pass"
	}
}

func computeSubjectTelemetry(subj model.SubjectMetadata, obsList []model.Observation) model.SubjectTelemetry {
	t := model.SubjectTelemetry{
		Subject: subj.Name,
		Role:    subj.Role,
		Cases:   len(obsList),
	}
	if len(obsList) == 0 {
		return t
	}
	durations := make([]int64, 0, len(obsList))
	for _, obs := range obsList {
		t.TotalInputBytes += obs.InputBytes
		t.TotalDurationNs += obs.DurationNs
		durations = append(durations, obs.DurationNs)
		if obs.PeakRSSBytes > t.PeakRSSBytes {
			t.PeakRSSBytes = obs.PeakRSSBytes
		}
		ratio := amplificationRatio(obs.InputBytes, obs.OutputBytes)
		if ratio > t.MaxAmplificationRatio {
			t.MaxAmplificationRatio = ratio
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.P50Ns = percentile(durations, 0.50)
	t.P95Ns = percentile(durations, 0.95)
	t.P99Ns = percentile(durations, 0.99)

	if t.TotalDurationNs > 0 {
		seconds := float64(t.TotalDurationNs) / 1e9
		t.DocsPerS = roundMetric(float64(len(obsList)) / seconds)
		mib := float64(t.TotalInputBytes) / (1024.0 * 1024.0)
		t.ThroughputMiBPerS = roundMetric(mib / seconds)
	}
	t.MaxAmplificationRatio = roundMetric(t.MaxAmplificationRatio)
	return t
}

func percentile(sorted []int64, q float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(q*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func amplificationRatio(inputBytes, outputBytes int64) float64 {
	if outputBytes <= 0 {
		return 0
	}
	denom := inputBytes
	if denom <= 0 {
		denom = 1
	}
	return float64(outputBytes) / float64(denom)
}

func roundMetric(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func finalizeReport(report *model.VerificationReport) (*model.VerificationReport, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("marshal verification report: %w", err)
	}
	if err := v0.ValidateVerificationReportBytes(raw); err != nil {
		return nil, fmt.Errorf("internal error: verification report failed schema validation: %w", err)
	}
	return report, nil
}

func parseCases(data []byte) ([]model.PackCase, error) {
	trimmed := bytes.TrimRight(data, "\r\n")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("cases.ndjson is empty")
	}
	lines := bytes.Split(trimmed, []byte{'\n'})
	cases := make([]model.PackCase, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for i, line := range lines {
		var c model.PackCase
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("line %d has trailing JSON", i+1)
		}
		if c.CaseID == "" || len(c.InputSHA256) != 64 || !strings.HasPrefix(c.InputPath, "inputs/") || filepath.ToSlash(filepath.Clean(c.InputPath)) != c.InputPath {
			return nil, fmt.Errorf("line %d (%s): invalid case metadata", i+1, c.CaseID)
		}
		if _, dup := seen[c.CaseID]; dup {
			return nil, fmt.Errorf("line %d: duplicate case_id %q", i+1, c.CaseID)
		}
		seen[c.CaseID] = struct{}{}
		cases = append(cases, c)
	}
	return cases, nil
}

func readRegularFile(root *os.Root, relPath string) ([]byte, error) {
	f, err := root.Open(relPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file (mode %s)", relPath, info.Mode().String())
	}
	return io.ReadAll(f)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
