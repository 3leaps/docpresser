//! Standalone Rust subject driver for serde_yaml implementing docpresser-driver/v0.
//!
//! It loads through serde_yaml's public untyped path: `from_slice::<Value>` for
//! single-document profiles, the `Deserializer::from_slice` document iterator
//! for stream profiles, and `Value::apply_merge` for the merge-apply profile.
//! serde_yaml parses with unsafe-libyaml, a Rust translation of libyaml; the
//! handshake reports both locked versions.
#![forbid(unsafe_code)]

use std::env;
use std::io::{self, Read};
use std::process;

use base64::Engine as _;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value as JsonValue};
use serde_yaml::Value;
use sha2::{Digest, Sha256};

const PROTOCOL_VERSION: &str = "docpresser-driver/v0";
const SUBJECT: &str = "serde-yaml-rust";
const SUBJECT_VERSION: &str = env!("SERDE_YAML_PIN");

#[derive(Serialize)]
struct HandshakeResponse {
    version: &'static str,
    subject: &'static str,
    subject_version: &'static str,
    language: &'static str,
    engine: String,
    formats: Vec<&'static str>,
    operations: Vec<&'static str>,
}

#[derive(Deserialize)]
struct CaseBudgets {
    #[allow(dead_code)]
    max_wall_ms: Option<i64>,
}

#[derive(Deserialize)]
struct CaseRequest {
    #[allow(dead_code)]
    version: String,
    case_id: String,
    #[allow(dead_code)]
    format: String,
    #[serde(default)]
    operation: String,
    #[serde(default)]
    profile: String,
    input_base64: String,
    budgets: Option<CaseBudgets>,
}

#[derive(Serialize)]
struct CaseResponse {
    version: &'static str,
    case_id: String,
    subject: &'static str,
    subject_version: &'static str,
    status: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    output_sha256: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    output_bytes: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    document_count: Option<usize>,
    #[serde(skip_serializing_if = "Option::is_none")]
    canonical_value: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error_phase: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error_code: Option<String>,
}

/// How a profile maps onto serde_yaml's public loading surface. serde_yaml
/// exposes no resource limits and no merge-key option: it keeps `<<` as an
/// ordinary key (the AsKey policy) unless `Value::apply_merge` is called. Only
/// profiles whose policy that surface honors exactly are supported; every
/// other name, including unknown ones, is unsupported rather than
/// approximated.
#[derive(Debug, PartialEq)]
enum Mode {
    /// `serde_yaml::from_slice::<Value>`: exactly one document.
    Single,
    /// `Deserializer::from_slice` iterated, one `Value` per document.
    Stream,
    /// `from_slice::<Value>` followed by `Value::apply_merge`.
    MergeApply,
    Unsupported,
}

fn mode_for_profile(profile: &str) -> Mode {
    match profile {
        // No profile requested: serde_yaml's ordinary single-document load.
        "" => Mode::Single,
        // Merge keys as ordinary keys, document stream.
        "yaml-core" => Mode::Stream,
        "yamlprims-core-merge-as-key" => Mode::Single,
        "yamlprims-core-merge-apply" => Mode::MergeApply,
        // Merge-key rejection, resource limits and unknown profiles cannot be
        // honored through the public untyped API.
        _ => Mode::Unsupported,
    }
}

fn engine() -> String {
    format!(
        "serde_yaml {SUBJECT_VERSION} (unsafe-libyaml {})",
        env!("UNSAFE_LIBYAML_VERSION")
    )
}

fn main() {
    let args: Vec<String> = env::args().skip(1).collect();
    if args.iter().any(|a| a == "--docpresser-handshake") {
        let resp = HandshakeResponse {
            version: PROTOCOL_VERSION,
            subject: SUBJECT,
            subject_version: SUBJECT_VERSION,
            language: "rust",
            engine: engine(),
            formats: vec!["yaml", "json"],
            operations: vec!["load_value"],
        };
        println!("{}", serde_json::to_string(&resp).unwrap_or_default());
        return;
    }

    let mut raw_req = String::new();
    if io::stdin().read_to_string(&mut raw_req).is_err() {
        process::exit(2);
    }
    let req: CaseRequest = match serde_json::from_str(&raw_req) {
        Ok(r) => r,
        Err(_) => process::exit(2),
    };

    if !req.operation.is_empty() && req.operation != "load_value" {
        let resp = unsupported_response(req.case_id, "unsupported_operation");
        println!("{}", serde_json::to_string(&resp).unwrap_or_default());
        return;
    }
    let input = match base64::engine::general_purpose::STANDARD.decode(&req.input_base64) {
        Ok(b) => b,
        Err(_) => process::exit(2),
    };
    let resp = execute_load_value(&req, &input);
    println!("{}", serde_json::to_string(&resp).unwrap_or_default());
}

fn load(mode: &Mode, input: &[u8]) -> Result<Vec<Value>, serde_yaml::Error> {
    match mode {
        Mode::Single => serde_yaml::from_slice::<Value>(input).map(|v| vec![v]),
        Mode::MergeApply => {
            let mut v = serde_yaml::from_slice::<Value>(input)?;
            v.apply_merge()?;
            Ok(vec![v])
        }
        Mode::Stream => serde_yaml::Deserializer::from_slice(input)
            .map(Value::deserialize)
            .collect(),
        Mode::Unsupported => unreachable!("handled by the caller"),
    }
}

fn execute_load_value(req: &CaseRequest, input: &[u8]) -> CaseResponse {
    let mode = mode_for_profile(&req.profile);
    if mode == Mode::Unsupported {
        return unsupported_response(req.case_id.clone(), "unsupported_profile");
    }
    match load(&mode, input) {
        Ok(docs) => {
            let out_docs: Vec<JsonValue> = docs.iter().map(notation).collect();
            let canonical = serde_json::to_string(&out_docs).unwrap_or_else(|_| "[]".to_string());
            let digest = format!("{:x}", Sha256::digest(canonical.as_bytes()));
            CaseResponse {
                version: PROTOCOL_VERSION,
                case_id: req.case_id.clone(),
                subject: SUBJECT,
                subject_version: SUBJECT_VERSION,
                status: "ok".to_string(),
                output_sha256: Some(digest),
                output_bytes: Some(canonical.len() as i64),
                document_count: Some(docs.len()),
                canonical_value: Some(canonical),
                error_phase: None,
                error_code: None,
            }
        }
        Err(err) => error_response(req, &err),
    }
}

/// The value notation shared with the yamlprims-rust driver, so equal loaded
/// values produce byte-identical canonical output.
fn notation(v: &Value) -> JsonValue {
    match v {
        Value::Null => json!({"t": "null"}),
        Value::Bool(b) => json!({"t": "bool", "v": b}),
        Value::Number(n) => {
            if let Some(i) = n.as_i64() {
                json!({"t": "int", "v": i.to_string()})
            } else if let Some(u) = n.as_u64() {
                json!({"t": "int", "v": u.to_string()})
            } else {
                let f = n.as_f64().unwrap_or(f64::NAN);
                let bits = if f.is_nan() {
                    0x7ff8_0000_0000_0000_u64
                } else {
                    f.to_bits()
                };
                json!({"bits": format!("{bits:016x}"), "t": "float"})
            }
        }
        Value::String(s) => json!({"t": "str", "v": s}),
        Value::Sequence(seq) => {
            let items: Vec<JsonValue> = seq.iter().map(notation).collect();
            json!({"items": items, "t": "seq"})
        }
        Value::Mapping(m) => {
            let entries: Vec<JsonValue> = m
                .iter()
                .map(|(k, v)| json!([notation(k), notation(v)]))
                .collect();
            json!({"entries": entries, "t": "map"})
        }
        Value::Tagged(t) => {
            json!({"inner": notation(&t.value), "t": "tagged", "tag": t.tag.to_string()})
        }
    }
}

fn unsupported_response(case_id: String, code: &str) -> CaseResponse {
    CaseResponse {
        version: PROTOCOL_VERSION,
        case_id,
        subject: SUBJECT,
        subject_version: SUBJECT_VERSION,
        status: "unsupported".to_string(),
        output_sha256: None,
        output_bytes: None,
        document_count: None,
        canonical_value: None,
        error_phase: None,
        error_code: Some(code.to_string()),
    }
}

/// serde_yaml's fixed diagnostics (pinned 0.9.34 `ErrorImpl` variants), with
/// the observation phase and code each maps to.
const FIXED_MESSAGES: &[(&str, &str, &str)] = &[
    ("recursion limit exceeded", "limit", "recursion_limit"),
    ("repetition limit exceeded", "limit", "repetition_limit"),
    ("unknown anchor", "structure", "unknown_anchor"),
    (
        "deserializing from YAML containing more than one document is not supported",
        "structure",
        "multiple_documents",
    ),
    (
        "expected a mapping or list of mappings for merging, but found scalar",
        "structure",
        "merge_rejected",
    ),
    (
        "unexpected tagged value in merge",
        "structure",
        "merge_rejected",
    ),
    (
        "expected a mapping for merging, but found scalar",
        "structure",
        "merge_rejected",
    ),
    (
        "expected a mapping for merging, but found sequence",
        "structure",
        "merge_rejected",
    ),
    ("EOF while parsing a value", "syntax", "end_of_stream"),
    ("empty YAML tag is not allowed", "syntax", "empty_tag"),
    (
        "failed to parse YAML number",
        "resolution",
        "invalid_number",
    ),
];

/// Endings of serde's `invalid_value` message for an explicitly tagged scalar
/// that does not parse (`!!bool`, `!!int`, `!!float`, `!!null`).
const INVALID_SCALAR_ENDINGS: &[&str] = &[
    ", expected a boolean",
    ", expected an integer",
    ", expected a float",
    ", expected null",
];

/// Ending of serde's `invalid_type` message when a scalar has no `Value`
/// representation, such as an integer outside the i64/u64 range, which serde
/// delivers as i128/u128.
const UNREPRESENTABLE_ENDING: &str = ", expected any YAML value";

/// Whether `msg` ends with a well-formed duplicate-key diagnostic from the
/// pinned `DuplicateKeyError`, at the start of the message or after a
/// `<path>: ` prefix.
fn is_duplicate_key(msg: &str) -> bool {
    let at_boundary = |idx: usize| idx == 0 || msg[..idx].ends_with(": ");
    for full in [
        "duplicate entry with null key",
        "duplicate entry in YAML map",
        "duplicate entry with key `true`",
        "duplicate entry with key `false`",
    ] {
        if msg.ends_with(full) && at_boundary(msg.len() - full.len()) {
            return true;
        }
    }
    let marker = "duplicate entry with key ";
    let mut search = msg.len();
    while let Some(idx) = msg[..search].rfind(marker) {
        let rest = &msg[idx + marker.len()..];
        if at_boundary(idx) && (is_debug_string(rest) || is_number_display(rest)) {
            return true;
        }
        search = idx;
    }
    false
}

/// A Rust `{:?}` string literal spanning all of `s`: opening and closing
/// quotes with every inner quote escaped.
fn is_debug_string(s: &str) -> bool {
    let Some(inner) = s.strip_prefix('"').and_then(|t| t.strip_suffix('"')) else {
        return false;
    };
    let mut escaped = false;
    for c in inner.chars() {
        match (escaped, c) {
            (true, _) => escaped = false,
            (false, '\\') => escaped = true,
            (false, '"') => return false,
            _ => {}
        }
    }
    !escaped
}

/// serde_yaml's `Display` of a `Number`: digits, sign, decimal point,
/// exponent, or `.inf`/`.nan` spellings, with no spaces or quotes.
fn is_number_display(s: &str) -> bool {
    !s.is_empty()
        && s.bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'.' | b'+' | b'-'))
}

/// Removes the ` at line N column M` suffix serde_yaml appends to a located
/// diagnostic.
fn strip_mark(msg: &str) -> &str {
    if let Some(idx) = msg.rfind(" at line ") {
        let tail = &msg[idx + " at line ".len()..];
        if let Some((line, col)) = tail.split_once(" column ") {
            let digits = |s: &str| !s.is_empty() && s.bytes().all(|b| b.is_ascii_digit());
            if digits(line) && digits(col) {
                return &msg[..idx];
            }
        }
    }
    msg
}

/// Classifies a serde_yaml error into an observation phase and code.
///
/// serde_yaml exposes no error kind, and its messages can quote input text
/// (key paths, duplicate keys, scalar values), so classification never
/// searches a message for a phrase:
/// - libyaml errors: the Debug form starts with `Error { kind: <CLASS>`,
///   which no other variant produces (they start with `Error("`).
/// - serde_yaml's own errors: the whole message equals a fixed diagnostic.
/// - an invalid explicitly tagged scalar: the message ends with serde's fixed
///   `, expected ...` text; a duplicate-key message cannot end that way
///   because a string key is quoted.
/// - a scalar with no `Value` representation (an integer outside i64/u64):
///   the message ends with serde's fixed `, expected any YAML value`.
/// - a duplicate key: the message ends with the pinned duplicate-key
///   diagnostic, checked as a whole (quoted string, number, bool, null or
///   collection key).
/// - anything else is reported as unclassified rather than guessed.
///
/// The grammar is that of the exact pinned version; the tests pin each case,
/// including inputs whose text imitates another diagnostic.
fn classify(err: &serde_yaml::Error) -> (&'static str, &'static str) {
    let debug = format!("{err:?}");
    if let Some(rest) = debug.strip_prefix("Error { kind: ") {
        return match rest.split([',', ' ']).next().unwrap_or("") {
            "READER" => ("input", "libyaml_reader"),
            "SCANNER" => ("syntax", "libyaml_scanner"),
            "PARSER" => ("syntax", "libyaml_parser"),
            "COMPOSER" => ("structure", "libyaml_composer"),
            "MEMORY" => ("limit", "libyaml_memory"),
            _ => ("syntax", "libyaml_other"),
        };
    }
    let display = err.to_string();
    let msg = strip_mark(&display);
    if let Some((_, phase, code)) = FIXED_MESSAGES.iter().find(|(m, _, _)| *m == msg) {
        return (phase, code);
    }
    if INVALID_SCALAR_ENDINGS.iter().any(|e| msg.ends_with(e)) {
        return ("resolution", "invalid_scalar");
    }
    if msg.ends_with(UNREPRESENTABLE_ENDING) {
        return ("resolution", "unrepresentable_value");
    }
    if is_duplicate_key(msg) {
        return ("structure", "duplicate_key");
    }
    ("resolution", "unclassified")
}

fn error_response(req: &CaseRequest, err: &serde_yaml::Error) -> CaseResponse {
    let (phase, code) = classify(err);
    let status = if phase == "limit" || req.budgets.is_some() {
        "refused_bounded"
    } else {
        "error"
    };
    CaseResponse {
        version: PROTOCOL_VERSION,
        case_id: req.case_id.clone(),
        subject: SUBJECT,
        subject_version: SUBJECT_VERSION,
        status: status.to_string(),
        output_sha256: None,
        output_bytes: None,
        document_count: None,
        canonical_value: None,
        error_phase: Some(phase.to_string()),
        error_code: Some(code.to_string()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn schema_error_phases() -> Vec<String> {
        let schema: serde_json::Value = serde_json::from_str(include_str!(
            "../../../schemas/docpresser/v0/observation.schema.json"
        ))
        .expect("observation schema parses");
        schema["properties"]["error_phase"]["enum"]
            .as_array()
            .expect("error_phase enum")
            .iter()
            .map(|v| v.as_str().expect("phase string").to_string())
            .collect()
    }

    fn run_case(profile: &str, input: &[u8]) -> CaseResponse {
        let req: CaseRequest = serde_json::from_value(json!({
            "version": PROTOCOL_VERSION,
            "case_id": "t",
            "format": "yaml",
            "operation": "load_value",
            "profile": profile,
            "input_base64": base64::engine::general_purpose::STANDARD.encode(input),
        }))
        .expect("request");
        execute_load_value(&req, input)
    }

    #[test]
    fn unsupported_responses_have_no_error_phase() {
        for code in ["unsupported_operation", "unsupported_profile"] {
            let resp = unsupported_response("c".into(), code);
            assert_eq!(resp.status, "unsupported");
            assert!(resp.error_phase.is_none());
        }
        let resp = run_case("yamlprims-core-limits", b"a: 1\n");
        assert_eq!(resp.status, "unsupported");
        assert_eq!(resp.error_code.as_deref(), Some("unsupported_profile"));
        assert!(resp.error_phase.is_none());
    }

    #[test]
    fn errors_map_to_schema_phases() {
        let phases = schema_error_phases();
        let deep = "[".repeat(5000);
        let cases: Vec<(&str, &[u8], &str, &str, &str)> = vec![
            ("", b"\xff\xfe", "error", "input", "libyaml_reader"),
            ("", b"a: [", "error", "syntax", "libyaml_parser"),
            ("", b"a: 'x", "error", "syntax", "libyaml_scanner"),
            ("", b"a: 1\na: 2\n", "error", "structure", "duplicate_key"),
            ("", b"*x\n", "error", "structure", "unknown_anchor"),
            (
                "",
                b"a\n---\nb\n",
                "error",
                "structure",
                "multiple_documents",
            ),
            (
                "",
                deep.as_bytes(),
                "refused_bounded",
                "limit",
                "recursion_limit",
            ),
            (
                "yamlprims-core-merge-apply",
                b"<<: 1\n",
                "error",
                "structure",
                "merge_rejected",
            ),
            // Input text that imitates another diagnostic must not select it.
            (
                "",
                b"recursion limit exceeded: 1\nrecursion limit exceeded: 2\n",
                "error",
                "structure",
                "duplicate_key",
            ),
            (
                "",
                b"repetition limit exceeded: 1\nrepetition limit exceeded: 2\n",
                "error",
                "structure",
                "duplicate_key",
            ),
            (
                "",
                b"unknown anchor: 1\nunknown anchor: 2\n",
                "error",
                "structure",
                "duplicate_key",
            ),
            (
                "",
                b"x: !!int \"recursion limit exceeded\"\n",
                "error",
                "resolution",
                "invalid_scalar",
            ),
            (
                "",
                b"x: !!int \"repetition limit exceeded\"\n",
                "error",
                "resolution",
                "invalid_scalar",
            ),
            (
                "",
                b"x: !!int \"duplicate entry\"\n",
                "error",
                "resolution",
                "invalid_scalar",
            ),
            (
                "",
                b"x: !!int \"unknown anchor\"\n",
                "error",
                "resolution",
                "invalid_scalar",
            ),
            (
                "",
                b"x: !!int \"EOF while parsing a value\"\n",
                "error",
                "resolution",
                "invalid_scalar",
            ),
            (
                "",
                b"x: !!bool \"for merging\"\n",
                "error",
                "resolution",
                "invalid_scalar",
            ),
            (
                "",
                b"x: !!float \"more than one document\"\n",
                "error",
                "resolution",
                "invalid_scalar",
            ),
            (
                "",
                b"x: !!null \"in merge\"\n",
                "error",
                "resolution",
                "invalid_scalar",
            ),
            (
                "",
                b"? \", expected an integer\"\n: 1\n? \", expected an integer\"\n: 2\n",
                "error",
                "structure",
                "duplicate_key",
            ),
            (
                "",
                b"recursion limit exceeded:\n  a: !!int x\n",
                "error",
                "resolution",
                "invalid_scalar",
            ),
            // Integers outside i64/u64 have no Value representation.
            (
                "",
                b"18446744073709551616\n",
                "error",
                "resolution",
                "unrepresentable_value",
            ),
            (
                "",
                b"-9223372036854775809\n",
                "error",
                "resolution",
                "unrepresentable_value",
            ),
            (
                "",
                b"0x10000000000000000\n",
                "error",
                "resolution",
                "unrepresentable_value",
            ),
            (
                "",
                b"a: !!int 18446744073709551616\n",
                "error",
                "resolution",
                "unrepresentable_value",
            ),
            (
                "yaml-core",
                b"a:\n  b: [1, -9223372036854775809]\n",
                "error",
                "resolution",
                "unrepresentable_value",
            ),
            (
                "",
                b"? 18446744073709551616\n: x\n",
                "error",
                "resolution",
                "unrepresentable_value",
            ),
            (
                "",
                b"\"duplicate entry with key 1\": 18446744073709551616\n",
                "error",
                "resolution",
                "unrepresentable_value",
            ),
            // Duplicate keys of every key kind.
            ("", b"1: a\n1: b\n", "error", "structure", "duplicate_key"),
            (
                "",
                b"true: a\ntrue: b\n",
                "error",
                "structure",
                "duplicate_key",
            ),
            ("", b"~: a\n~: b\n", "error", "structure", "duplicate_key"),
            (
                "",
                b"[1]: a\n[1]: b\n",
                "error",
                "structure",
                "duplicate_key",
            ),
            (
                "",
                b"outer:\n  'q\"x': 1\n  'q\"x': 2\n",
                "error",
                "structure",
                "duplicate_key",
            ),
        ];
        for (profile, input, status, phase, code) in cases {
            let resp = run_case(profile, input);
            let label = String::from_utf8_lossy(&input[..input.len().min(16)]).into_owned();
            assert_eq!(resp.status, status, "status for {label:?}");
            assert_eq!(
                resp.error_phase.as_deref(),
                Some(phase),
                "phase for {label:?}"
            );
            assert_eq!(resp.error_code.as_deref(), Some(code), "code for {label:?}");
            assert!(
                phases.iter().any(|p| p == phase),
                "{phase} not in schema enum"
            );
        }
    }

    #[test]
    fn notation_matches_yamlprims_rust_shape() {
        let resp = run_case("", b"a: NO\nb: \"NO\"\nc: 1\nd: 1.5\ne: ~\nf: [true]\n");
        assert_eq!(resp.status, "ok");
        assert_eq!(
            resp.canonical_value.as_deref(),
            Some(concat!(
                r#"[{"entries":[[{"t":"str","v":"a"},{"t":"str","v":"NO"}],"#,
                r#"[{"t":"str","v":"b"},{"t":"str","v":"NO"}],"#,
                r#"[{"t":"str","v":"c"},{"t":"int","v":"1"}],"#,
                r#"[{"t":"str","v":"d"},{"bits":"3ff8000000000000","t":"float"}],"#,
                r#"[{"t":"str","v":"e"},{"t":"null"}],"#,
                r#"[{"t":"str","v":"f"},{"items":[{"t":"bool","v":true}],"t":"seq"}]],"t":"map"}]"#
            ))
        );
    }

    #[test]
    fn only_honored_profiles_are_supported() {
        let merge = b"<<: {a: 1}\nb: 2\n";
        for profile in [
            "yamlprims-core",
            "yamlprims-core-stream",
            "yamlprims-core-limits",
            "yamlprims-core-depth128",
            "yamlprims-core-next",
            "yamlprims-next-limits",
            "yaml-core-strict",
            "YAML-CORE",
        ] {
            let resp = run_case(profile, merge);
            assert_eq!(resp.status, "unsupported", "{profile}");
            assert_eq!(
                resp.error_code.as_deref(),
                Some("unsupported_profile"),
                "{profile}"
            );
            assert!(resp.error_phase.is_none(), "{profile}");
        }
        // AsKey profiles keep `<<` as an ordinary key.
        for profile in ["", "yaml-core", "yamlprims-core-merge-as-key"] {
            let resp = run_case(profile, merge);
            assert_eq!(resp.status, "ok", "{profile}");
            let v = resp.canonical_value.expect("value");
            assert!(v.contains(r#"{"t":"str","v":"<<"}"#), "{profile}: {v}");
        }
        let resp = run_case("yamlprims-core-merge-apply", merge);
        assert_eq!(resp.status, "ok");
        assert!(!resp.canonical_value.expect("value").contains("<<"));
    }

    #[test]
    fn stream_profiles_return_every_document() {
        let resp = run_case("yaml-core", b"a\n---\nb\n");
        assert_eq!(resp.status, "ok");
        assert_eq!(resp.document_count, Some(2));
    }
}
