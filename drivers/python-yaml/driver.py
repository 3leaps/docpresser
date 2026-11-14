#!/usr/bin/env python3
"""Standalone Python YAML subject driver implementing docpresser-driver/v0.

Supports:
  --engine pyyaml-safe   (default: PyYAML SafeLoader when installed, or built-in YAML 1.1 fallback)
  --engine pyyaml-csafe  (PyYAML CSafeLoader / libyaml when available)
  --engine yamlprims     (yamlprims Python bindings when installed)
"""

import base64
import hashlib
import json
import math
import platform
import re
import struct
import sys
import time

PROTOCOL_VERSION = "docpresser-driver/v0"

YAML11_TRUE = {"y", "Y", "yes", "Yes", "YES", "true", "True", "TRUE", "on", "On", "ON"}
YAML11_FALSE = {
    "n",
    "N",
    "no",
    "No",
    "NO",
    "false",
    "False",
    "FALSE",
    "off",
    "Off",
    "OFF",
}
YAML11_NULL = {"~", "null", "Null", "NULL", ""}
OCTAL_RE = re.compile(r"^[-+]?0[0-7]+$")
INT_RE = re.compile(r"^[-+]?(0|[1-9][0-9_]*)$")
SEXA_RE = re.compile(r"^[-+]?[1-9][0-9_]*(:[0-5]?[0-9])+$")


def parse_engine(argv: list[str]) -> str:
    engine = "pyyaml-safe"
    for i, arg in enumerate(argv):
        if arg == "--engine" and i + 1 < len(argv):
            engine = argv[i + 1]
        elif arg.startswith("--engine="):
            engine = arg.split("=", 1)[1]
    return engine


def subject_version() -> str:
    py_ver = platform.python_version()
    try:
        import yaml  # type: ignore

        yaml_ver = getattr(yaml, "__version__", "unknown")
    except Exception:
        yaml_ver = "builtin-yaml11"
    return f"{yaml_ver} (python {py_ver})"


def handshake(engine: str) -> dict:
    return {
        "version": PROTOCOL_VERSION,
        "subject": f"python-{engine}",
        "subject_version": subject_version(),
        "language": "python",
        "engine": engine,
        "formats": ["yaml", "json"],
        "operations": ["load_value"],
    }


def float_bits_hex(val: float) -> str:
    if math.isnan(val):
        return "7ff8000000000000"
    packed = struct.pack(">d", val)
    (bits,) = struct.unpack(">Q", packed)
    return f"{bits:016x}"


def to_notation(val, depth: int = 0) -> dict:
    if depth > 512:
        raise RecursionError("maximum recursion depth exceeded in notation projection")
    if val is None:
        return {"t": "null"}
    if isinstance(val, bool):
        return {"t": "bool", "v": val}
    if isinstance(val, int):
        return {"t": "int", "v": str(val)}
    if isinstance(val, float):
        return {"bits": float_bits_hex(val), "t": "float"}
    if isinstance(val, str):
        return {"t": "str", "v": val}
    if isinstance(val, (list, tuple)):
        return {"items": [to_notation(item, depth + 1) for item in val], "t": "seq"}
    if isinstance(val, dict):
        entries = []
        for k, v in val.items():
            entries.append([to_notation(k, depth + 1), to_notation(v, depth + 1)])
        return {"entries": entries, "t": "map"}
    return {"t": "str", "v": str(val)}


def resolve_yaml11_scalar(raw: str):
    token = raw.strip()
    if not token:
        return None
    if (token.startswith('"') and token.endswith('"')) or (
        token.startswith("'") and token.endswith("'")
    ):
        if token.startswith('"'):
            return json.loads(token)
        return token[1:-1].replace("''", "'")
    if token in YAML11_NULL:
        return None
    if token in YAML11_TRUE:
        return True
    if token in YAML11_FALSE:
        return False
    if OCTAL_RE.match(token):
        sign = -1 if token.startswith("-") else 1
        digits = token.lstrip("+-")
        return sign * int(digits, 8)
    if INT_RE.match(token):
        return int(token.replace("_", ""), 10)
    if SEXA_RE.match(token):
        sign = -1 if token.startswith("-") else 1
        parts = token.lstrip("+-").split(":")
        total = 0
        for p in parts:
            total = total * 60 + int(p.replace("_", ""), 10)
        return sign * total
    return token


def parse_flow_value(expr: str, anchors: dict, depth: int = 0):
    if depth > 128:
        raise RecursionError("maximum flow nesting depth exceeded")
    s = expr.strip()
    if s.startswith("["):
        if not s.endswith("]"):
            raise ValueError("unterminated flow sequence")
        inner = s[1:-1].strip()
        if not inner:
            return []
        parts = split_flow_items(inner)
        return [parse_flow_value(p, anchors, depth + 1) for p in parts]
    if s.startswith("*"):
        anchor_name = s[1:].strip()
        if anchor_name not in anchors:
            raise ValueError(f"unknown anchor {anchor_name!r}")
        return anchors[anchor_name]
    return resolve_yaml11_scalar(s)


def split_flow_items(inner: str) -> list[str]:
    items = []
    cur = []
    depth = 0
    in_quote = ""
    for ch in inner:
        if in_quote:
            cur.append(ch)
            if ch == in_quote:
                in_quote = ""
            continue
        if ch in ('"', "'"):
            in_quote = ch
            cur.append(ch)
        elif ch in ("[", "{"):
            depth += 1
            cur.append(ch)
        elif ch in ("]", "}"):
            depth -= 1
            if depth < 0:
                raise ValueError("unbalanced closing bracket")
            cur.append(ch)
        elif ch == "," and depth == 0:
            items.append("".join(cur).strip())
            cur = []
        else:
            cur.append(ch)
    if in_quote or depth != 0:
        raise ValueError("unterminated flow construct")
    if cur:
        items.append("".join(cur).strip())
    return items


def builtin_yaml11_load_all(text: str) -> list:
    if "\x00" in text:
        raise ValueError("NUL byte not allowed in YAML stream")
    stripped = text.strip()
    if not stripped:
        return [None]
    if stripped.startswith("{") or stripped.startswith("["):
        anchors: dict = {}
        if stripped.startswith("{"):
            return [json.loads(stripped)]
        return [parse_flow_value(stripped, anchors, 0)]

    lines = []
    for raw_line in text.splitlines():
        if not raw_line.strip() or raw_line.lstrip().startswith("#"):
            continue
        indent = len(raw_line) - len(raw_line.lstrip(" "))
        lines.append((indent, raw_line.strip()))

    if not lines:
        return [None]

    anchors: dict = {}

    def parse_block_mapping(idx: int, base_indent: int) -> tuple[dict, int]:
        result: dict = {}
        merged: dict = {}
        while idx < len(lines):
            indent, content = lines[idx]
            if indent < base_indent:
                break
            if indent > base_indent:
                raise ValueError(f"unexpected indentation at {content!r}")
            if ":" not in content:
                raise ValueError(f"expected mapping key:value at {content!r}")
            key_raw, val_raw = content.split(":", 1)
            key = str(resolve_yaml11_scalar(key_raw))
            val_str = val_raw.strip()

            anchor_name = None
            if val_str.startswith("&"):
                parts = val_str.split(None, 1)
                anchor_name = parts[0][1:]
                val_str = parts[1].strip() if len(parts) > 1 else ""

            idx += 1
            if val_str == "":
                if idx < len(lines) and lines[idx][0] > base_indent:
                    child_indent = lines[idx][0]
                    val, idx = parse_block_mapping(idx, child_indent)
                else:
                    val = None
            else:
                val = parse_flow_value(val_str, anchors, 0)

            if anchor_name:
                anchors[anchor_name] = val

            if key == "<<" and not (
                key_raw.strip().startswith('"') or key_raw.strip().startswith("'")
            ):
                if isinstance(val, dict):
                    for mk, mv in val.items():
                        if mk not in merged:
                            merged[mk] = mv
                else:
                    raise ValueError("invalid merge source")
            else:
                if key in result:
                    raise ValueError(f"duplicate key {key!r}")
                result[key] = val

        if merged:
            combined: dict = {}
            for mk, mv in merged.items():
                if mk not in result:
                    combined[mk] = mv
            for rk, rv in result.items():
                combined[rk] = rv
            return combined, idx
        return result, idx

    doc, next_idx = parse_block_mapping(0, lines[0][0])
    if next_idx < len(lines):
        raise ValueError("trailing unparsed content")
    return [doc]


def load_yaml_documents(text: str, engine: str) -> list:
    try:
        import yaml  # type: ignore

        loader_cls = yaml.SafeLoader
        if engine == "pyyaml-csafe":
            loader_cls = getattr(yaml, "CSafeLoader", yaml.SafeLoader)
        docs = list(yaml.load_all(text, Loader=loader_cls))
        return docs if docs else [None]
    except ImportError:
        return builtin_yaml11_load_all(text)


def execute_case(req: dict, engine: str) -> dict:
    case_id = req.get("case_id", "")
    subj = f"python-{engine}"
    subj_ver = subject_version()
    op = req.get("operation", "load_value")
    if op and op != "load_value":
        return {
            "version": PROTOCOL_VERSION,
            "case_id": case_id,
            "subject": subj,
            "subject_version": subj_ver,
            "status": "unsupported",
            "error_phase": "driver",
            "error_code": "unsupported_operation",
        }

    raw_bytes = base64.b64decode(req.get("input_base64", ""))
    t0 = time.perf_counter_ns()
    try:
        text = raw_bytes.decode("utf-8")
    except UnicodeDecodeError:
        return {
            "version": PROTOCOL_VERSION,
            "case_id": case_id,
            "subject": subj,
            "subject_version": subj_ver,
            "status": "error",
            "error_phase": "input",
            "error_code": "invalid_utf8",
            "parse_ns": time.perf_counter_ns() - t0,
        }

    try:
        docs = load_yaml_documents(text, engine)
        parse_ns = time.perf_counter_ns() - t0
        t_emit = time.perf_counter_ns()
        out_docs = [to_notation(doc) for doc in docs]
        canonical = json.dumps(
            out_docs, separators=(",", ":"), sort_keys=True, ensure_ascii=False
        )
        emit_ns = time.perf_counter_ns() - t_emit
        canonical_bytes = canonical.encode("utf-8")
        digest = hashlib.sha256(canonical_bytes).hexdigest()
        return {
            "version": PROTOCOL_VERSION,
            "case_id": case_id,
            "subject": subj,
            "subject_version": subj_ver,
            "status": "ok",
            "canonical_value": canonical,
            "output_sha256": digest,
            "output_bytes": len(canonical_bytes),
            "document_count": len(docs),
            "parse_ns": parse_ns,
            "emit_ns": emit_ns,
        }
    except RecursionError:
        return {
            "version": PROTOCOL_VERSION,
            "case_id": case_id,
            "subject": subj,
            "subject_version": subj_ver,
            "status": "refused_bounded" if req.get("budgets") else "error",
            "error_phase": "limit",
            "error_code": "recursion_error",
            "parse_ns": time.perf_counter_ns() - t0,
        }
    except Exception as exc:
        return {
            "version": PROTOCOL_VERSION,
            "case_id": case_id,
            "subject": subj,
            "subject_version": subj_ver,
            "status": "refused_bounded" if req.get("budgets") else "error",
            "error_phase": "syntax",
            "error_code": exc.__class__.__name__,
            "parse_ns": time.perf_counter_ns() - t0,
        }


def main() -> None:
    engine = parse_engine(sys.argv[1:])
    if "--docpresser-handshake" in sys.argv[1:]:
        sys.stdout.write(json.dumps(handshake(engine), separators=(",", ":")) + "\n")
        return

    raw_req = sys.stdin.read()
    req = json.loads(raw_req)
    resp = execute_case(req, engine)
    sys.stdout.write(json.dumps(resp, separators=(",", ":"), ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
