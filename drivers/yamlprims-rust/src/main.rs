//! Standalone Rust subject driver for 3leaps/yamlprims implementing docpresser-driver/v0.
#![forbid(unsafe_code)]

use std::env;
use std::io::{self, Read};
use std::process;

use base64::Engine as _;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value as JsonValue};
use sha2::{Digest, Sha256};
use yamlprims::{Error, ErrorKind, LimitKind, Limits, MergeKeys, Node, Options, Value};

const PROTOCOL_VERSION: &str = "docpresser-driver/v0";

#[derive(Serialize)]
struct HandshakeResponse {
    version: &'static str,
    subject: &'static str,
    subject_version: &'static str,
    language: &'static str,
    engine: &'static str,
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

fn main() {
    let args: Vec<String> = env::args().skip(1).collect();
    if args.iter().any(|a| a == "--docpresser-handshake") {
        let resp = HandshakeResponse {
            version: PROTOCOL_VERSION,
            subject: "yamlprims-rust",
            subject_version: env!("YAMLPRIMS_PIN"),
            language: "rust",
            engine: "3leaps/yamlprims",
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
        let resp = CaseResponse {
            version: PROTOCOL_VERSION,
            case_id: req.case_id,
            subject: "yamlprims-rust",
            subject_version: env!("YAMLPRIMS_PIN"),
            status: "unsupported".to_string(),
            output_sha256: None,
            output_bytes: None,
            document_count: None,
            canonical_value: None,
            error_phase: Some("driver".to_string()),
            error_code: Some("unsupported_operation".to_string()),
        };
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

fn execute_load_value(req: &CaseRequest, input: &[u8]) -> CaseResponse {
    let (opts, single) = options_for_profile(&req.profile);
    let docs_res = if single {
        yamlprims::load_bytes(input, &opts).map(|d| vec![d])
    } else {
        yamlprims::load_all_bytes(input, &opts)
    };

    match docs_res {
        Ok(docs) => {
            let out_docs: Vec<JsonValue> = docs.iter().map(|d| notation(d.root())).collect();
            let canonical = serde_json::to_string(&out_docs).unwrap_or_else(|_| "[]".to_string());
            let mut hasher = Sha256::new();
            hasher.update(canonical.as_bytes());
            let digest = format!("{:x}", hasher.finalize());
            let out_len = canonical.len() as i64;
            CaseResponse {
                version: PROTOCOL_VERSION,
                case_id: req.case_id.clone(),
                subject: "yamlprims-rust",
                subject_version: env!("YAMLPRIMS_PIN"),
                status: "ok".to_string(),
                output_sha256: Some(digest),
                output_bytes: Some(out_len),
                document_count: Some(docs.len()),
                canonical_value: Some(canonical),
                error_phase: None,
                error_code: None,
            }
        }
        Err(err) => yaml_error_to_response(req, &err),
    }
}

fn options_for_profile(profile: &str) -> (Options, bool) {
    let mut opts = Options::default();
    let mut single = true;
    match profile {
        "yaml-core" => {
            opts.merge_keys = MergeKeys::AsKey;
            single = false;
        }
        "yamlprims-core-stream" => {
            opts.merge_keys = MergeKeys::Reject;
            single = false;
        }
        "yamlprims-core-merge-apply" => {
            opts.merge_keys = MergeKeys::Apply;
        }
        "yamlprims-core-merge-as-key" => {
            opts.merge_keys = MergeKeys::AsKey;
        }
        "yamlprims-core-limits" => {
            let mut limits = Limits::default();
            limits.max_input_bytes = 65536;
            limits.max_depth = 8;
            limits.max_nodes = 64;
            limits.max_expanded_nodes = 32;
            limits.max_alias_events = 8;
            limits.max_owned_bytes = 8192;
            limits.max_scalar_bytes = 1024;
            limits.max_key_bytes = 512;
            limits.max_mapping_entries = 16;
            limits.max_documents = 4;
            limits.max_output_bytes = 65536;
            limits.max_warnings = 16;
            opts.limits = limits;
        }
        _ => {}
    }
    (opts, single)
}

fn notation(n: &Node) -> JsonValue {
    match n.value() {
        Value::Null => json!({"t": "null"}),
        Value::Bool(b) => json!({"t": "bool", "v": b}),
        Value::Int(i) => json!({"t": "int", "v": i.to_string()}),
        Value::Float(f) => {
            let bits = if f.is_nan() {
                0x7ff8_0000_0000_0000_u64
            } else {
                f.to_bits()
            };
            json!({"bits": format!("{bits:016x}"), "t": "float"})
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
            json!({"inner": notation(t.value()), "t": "tagged", "tag": t.tag().uri()})
        }
        _ => json!({"t": "unknown"}),
    }
}

fn yaml_error_to_response(req: &CaseRequest, err: &Error) -> CaseResponse {
    let phase = match &err.kind {
        ErrorKind::InvalidUtf8 { .. } => "input",
        ErrorKind::LimitExceeded { limit, .. } if *limit == LimitKind::MaxInputBytes => "input",
        ErrorKind::Syntax(_) => "syntax",
        ErrorKind::LimitExceeded { .. } => "limit",
        ErrorKind::DuplicateKey
        | ErrorKind::InvalidKey
        | ErrorKind::UnknownAnchor
        | ErrorKind::RecursiveAlias
        | ErrorKind::MergeRejected
        | ErrorKind::MergeSource
        | ErrorKind::MultipleDocuments => "structure",
        _ => "resolution",
    };
    let status = if phase == "limit" || req.budgets.is_some() {
        "refused_bounded"
    } else {
        "error"
    };
    CaseResponse {
        version: PROTOCOL_VERSION,
        case_id: req.case_id.clone(),
        subject: "yamlprims-rust",
        subject_version: env!("YAMLPRIMS_PIN"),
        status: status.to_string(),
        output_sha256: None,
        output_bytes: None,
        document_count: None,
        canonical_value: None,
        error_phase: Some(phase.to_string()),
        error_code: Some(err.kind.name().to_string()),
    }
}
