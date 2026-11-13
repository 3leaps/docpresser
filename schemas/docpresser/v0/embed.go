// Package v0 embeds and validates the governed docpresser/v0 JSON schemas.
package v0

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

//go:embed *.schema.json
var assets embed.FS

var names = []string{
	"observation.schema.json",
	"pack.schema.json",
	"run-manifest.schema.json",
	"verification-report.schema.json",
}

const (
	maxEnvelopeBytes = 4 << 20
	maxJSONDepth     = 64
	maxJSONValues    = 131_072
)

type validatorResult struct {
	schema *jsonschema.Schema
	err    error
}

var validatorCache sync.Map

// Schema returns an embedded schema asset by file name.
func Schema(name string) ([]byte, error) {
	data, err := assets.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read embedded schema %q: %w", name, err)
	}
	return data, nil
}

// Names returns the sorted inventory of governed schema assets.
func Names() []string {
	result := append([]string(nil), names...)
	sort.Strings(result)
	return result
}

// SchemaSHA256 returns the inventory SHA-256 over every embedded schema name and byte stream.
func SchemaSHA256() string {
	h := sha256.New()
	for _, name := range Names() {
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{0})
		data, err := Schema(name)
		if err != nil {
			panic(err)
		}
		_, _ = h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ValidatePackBytes validates raw JSON bytes against pack.schema.json.
func ValidatePackBytes(data []byte) error {
	return ValidateBytes(data, "pack.schema.json")
}

// ValidateObservationBytes validates raw JSON bytes against observation.schema.json.
func ValidateObservationBytes(data []byte) error {
	return ValidateBytes(data, "observation.schema.json")
}

// ValidateRunManifestBytes validates raw JSON bytes against run-manifest.schema.json.
func ValidateRunManifestBytes(data []byte) error {
	return ValidateBytes(data, "run-manifest.schema.json")
}

// ValidateVerificationReportBytes validates raw JSON bytes against verification-report.schema.json.
func ValidateVerificationReportBytes(data []byte) error {
	return ValidateBytes(data, "verification-report.schema.json")
}

// ValidateBytes validates raw JSON bytes against the named embedded schema,
// rejecting duplicate keys, trailing JSON, non-UTF-8 bytes, and excessive nesting.
func ValidateBytes(data []byte, schemaName string) error {
	if len(data) > maxEnvelopeBytes {
		return fmt.Errorf("%s exceeds %d byte limit", schemaName, maxEnvelopeBytes)
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("%s is not valid UTF-8", schemaName)
	}
	if err := validateJSONStructure(data); err != nil {
		return err
	}
	compiled, err := schemaFor(schemaName)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("decode %s JSON: %w", schemaName, err)
	}
	if err := compiled.Validate(doc); err != nil {
		return fmt.Errorf("validate %s: %w", schemaName, err)
	}
	return nil
}

// Decode validates raw JSON bytes against schemaName and decodes into out with DisallowUnknownFields.
func Decode(data []byte, schemaName string, out any) error {
	if err := ValidateBytes(data, schemaName); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", schemaName, err)
	}
	if err := requireEOF(decoder); err != nil {
		return fmt.Errorf("decode %s: %w", schemaName, err)
	}
	return nil
}

func schemaFor(name string) (*jsonschema.Schema, error) {
	if cached, ok := validatorCache.Load(name); ok {
		result := cached.(validatorResult)
		return result.schema, result.err
	}
	schemaData, err := Schema(name)
	if err == nil {
		url := "https://schemas.3leaps.dev/docpresser/v0/" + name
		compiler := jsonschema.NewCompiler()
		compiler.Draft = jsonschema.Draft2020
		if err = compiler.AddResource(url, bytes.NewReader(schemaData)); err == nil {
			var compiled *jsonschema.Schema
			compiled, err = compiler.Compile(url)
			if err == nil {
				result := validatorResult{schema: compiled}
				actual, loaded := validatorCache.LoadOrStore(name, result)
				if loaded {
					result = actual.(validatorResult)
				}
				return result.schema, result.err
			}
		}
	}
	return nil, fmt.Errorf("compile %s: %w", name, err)
}

func validateJSONStructure(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	values := 0
	if err := consumeValue(decoder, 0, &values); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return requireEOF(decoder)
}

func consumeValue(decoder *json.Decoder, depth int, values *int) error {
	if depth >= maxJSONDepth {
		return fmt.Errorf("JSON nesting exceeds %d levels", maxJSONDepth)
	}
	*values++
	if *values > maxJSONValues {
		return fmt.Errorf("JSON contains more than %d values", maxJSONValues)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := keys[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			keys[key] = struct{}{}
			if err := consumeValue(decoder, depth+1, values); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := consumeValue(decoder, depth+1, values); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected delimiter %q", delimiter)
	}
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("trailing JSON value")
	}
	return err
}
