package v0

import (
	"testing"
)

func TestSchemasCompileAndDigest(t *testing.T) {
	digest := SchemaSHA256()
	if len(digest) != 64 {
		t.Fatalf("SchemaSHA256() = %q, want 64-char hex digest", digest)
	}
	if err := ValidatePackBytes([]byte(`{}`)); err == nil {
		t.Fatal("ValidatePackBytes({}) succeeded, want error")
	}
	if err := ValidateObservationBytes([]byte(`{}`)); err == nil {
		t.Fatal("ValidateObservationBytes({}) succeeded, want error")
	}
	if err := ValidateRunManifestBytes([]byte(`{}`)); err == nil {
		t.Fatal("ValidateRunManifestBytes({}) succeeded, want error")
	}
	if err := ValidateVerificationReportBytes([]byte(`{}`)); err == nil {
		t.Fatal("ValidateVerificationReportBytes({}) succeeded, want error")
	}
}
