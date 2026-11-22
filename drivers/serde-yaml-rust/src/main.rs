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
/// exposes no resource limits or merge-key policy options, so profiles that
/// depend on them are unsupported rather than silently approximated.
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
        "yaml-core" | "yamlprims-core-stream" => Mode::Stream,
        "yamlprims-core-merge-apply" => Mode::MergeApply,
        "yamlprims-core-merge-as-key" => Mode::Single,
        p if p.starts_with("yamlprims-core-") => Mode::Unsupported,
        _ => Mode::Single,
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

/// Classifies a serde_yaml error into an observation phase and code.
///
/// serde_yaml exposes no error kind. Its Debug output for errors raised by
/// libyaml names the libyaml error class (`kind: SCANNER` and so on), and its
/// own errors use fixed messages. Both are specific to the exact pinned
/// version; the tests pin each mapping.
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
    let msg = err.to_string();
    let has = |s: &str| msg.contains(s);
    if has("recursion limit exceeded") {
        ("limit", "recursion_limit")
    } else if has("repetition limit exceeded") {
        ("limit", "repetition_limit")
    } else if has("duplicate entry") {
        ("structure", "duplicate_key")
    } else if has("unknown anchor") {
        ("structure", "unknown_anchor")
    } else if has("more than one document") {
        ("structure", "multiple_documents")
    } else if has("for merging") || has("in merge") {
        ("structure", "merge_rejected")
    } else if has("EOF while parsing") {
        ("syntax", "end_of_stream")
    } else {
        ("resolution", "other")
    }
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
    fn stream_profiles_return_every_document() {
        let resp = run_case("yaml-core", b"a\n---\nb\n");
        assert_eq!(resp.status, "ok");
        assert_eq!(resp.document_count, Some(2));
    }
}
