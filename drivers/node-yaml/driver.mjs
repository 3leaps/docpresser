#!/usr/bin/env node
// Standalone Node.js subject driver implementing docpresser-driver/v0.

import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const PROTOCOL_VERSION = "docpresser-driver/v0";

// engine is { ok: true, api, version } for a loaded js-yaml, or
// { ok: false, error } with whatever value the load threw (which may be falsy).
// It is loaded once, with the same ESM import every YAML case uses, so the
// handshake only succeeds for an engine that actually loads.
let engine = { ok: false, error: new Error("js-yaml not loaded yet") };

// describeError renders any thrown value, including non-Error and falsy ones.
function describeError(err) {
	try {
		if (err instanceof Error) {
			return err.message || err.name;
		}
		return `non-Error value thrown: ${String(err)}`;
	} catch {
		return "unprintable value thrown";
	}
}

function packageVersion(entryPath) {
	let dir = path.dirname(entryPath);
	for (;;) {
		const candidate = path.join(dir, "package.json");
		if (fs.existsSync(candidate)) {
			const pkg = JSON.parse(fs.readFileSync(candidate, "utf8"));
			if (pkg.name === "js-yaml" && typeof pkg.version === "string") {
				return pkg.version;
			}
		}
		const parent = path.dirname(dir);
		if (parent === dir) {
			return "";
		}
		dir = parent;
	}
}

async function loadEngine() {
	try {
		const mod = await import("js-yaml");
		const api = typeof mod.loadAll === "function" ? mod : mod.default;
		if (!api || typeof api.loadAll !== "function") {
			throw new Error("js-yaml does not export loadAll");
		}
		const version = packageVersion(
			fileURLToPath(import.meta.resolve("js-yaml")),
		);
		if (!version) {
			throw new Error("cannot read the js-yaml package version");
		}
		return { ok: true, api, version };
	} catch (error) {
		return { ok: false, error };
	}
}

function subjectVersion() {
	const v = engine.ok ? engine.version : "unavailable";
	return `js-yaml ${v} (node ${process.version})`;
}

function canonicalizeJCSValue(val) {
	if (val === null || typeof val === "boolean") {
		return JSON.stringify(val);
	}
	if (typeof val === "number") {
		if (!Number.isFinite(val)) {
			throw new Error("non_finite_number");
		}
		if (Object.is(val, -0)) {
			return "0";
		}
		return JSON.stringify(val);
	}
	if (typeof val === "string") {
		return JSON.stringify(val);
	}
	if (Array.isArray(val)) {
		return "[" + val.map((item) => canonicalizeJCSValue(item)).join(",") + "]";
	}
	if (typeof val === "object") {
		const keys = Object.keys(val).sort();
		const parts = keys.map(
			(k) => JSON.stringify(k) + ":" + canonicalizeJCSValue(val[k]),
		);
		return "{" + parts.join(",") + "}";
	}
	throw new Error("unsupported_jcs_type");
}

function floatBitsHex(val) {
	if (Number.isNaN(val)) {
		return "7ff8000000000000";
	}
	const buf = Buffer.alloc(8);
	buf.writeDoubleBE(val, 0);
	return buf.toString("hex");
}

function toNotation(val) {
	if (val === null || val === undefined) {
		return { t: "null" };
	}
	if (typeof val === "boolean") {
		return { t: "bool", v: val };
	}
	if (typeof val === "number") {
		if (Number.isInteger(val)) {
			return { t: "int", v: String(val) };
		}
		return { bits: floatBitsHex(val), t: "float" };
	}
	if (typeof val === "string") {
		return { t: "str", v: val };
	}
	if (Array.isArray(val)) {
		return { items: val.map((x) => toNotation(x)), t: "seq" };
	}
	if (typeof val === "object") {
		const entries = Object.entries(val).map(([k, v]) => [
			toNotation(k),
			toNotation(v),
		]);
		return { entries, t: "map" };
	}
	return { t: "str", v: String(val) };
}

async function main() {
	engine = await loadEngine();
	const args = process.argv.slice(2);
	if (args.includes("--docpresser-handshake")) {
		if (!engine.ok) {
			const reason = describeError(engine.error).split("\n")[0];
			process.stderr.write(
				`node-yaml driver: js-yaml cannot be loaded: ${reason}\n`,
			);
			process.exit(2);
		}
		const resp = {
			version: PROTOCOL_VERSION,
			subject: "node-yaml",
			subject_version: subjectVersion(),
			language: "javascript",
			engine: "node-js-yaml",
			formats: ["yaml", "json", "jcs"],
			operations: ["load_value", "project_jcs"],
		};
		process.stdout.write(JSON.stringify(resp) + "\n");
		return;
	}

	const rawReq = fs.readFileSync(0, "utf8");
	const req = JSON.parse(rawReq);
	const caseId = req.case_id || "";
	const inputBuf = Buffer.from(req.input_base64 || "", "base64");
	const text = inputBuf.toString("utf8");

	if (req.operation === "project_jcs") {
		try {
			const parsed = JSON.parse(text);
			const canonical = canonicalizeJCSValue(parsed);
			const canonicalBytes = Buffer.from(canonical, "utf8");
			const digest = crypto
				.createHash("sha256")
				.update(canonicalBytes)
				.digest("hex");
			process.stdout.write(
				JSON.stringify({
					version: PROTOCOL_VERSION,
					case_id: caseId,
					subject: "node-yaml",
					subject_version: subjectVersion(),
					status: "ok",
					canonical_value: canonical,
					output_sha256: digest,
					output_bytes: canonicalBytes.length,
					document_count: 1,
				}) + "\n",
			);
		} catch {
			process.stdout.write(
				JSON.stringify({
					version: PROTOCOL_VERSION,
					case_id: caseId,
					subject: "node-yaml",
					subject_version: subjectVersion(),
					status: "error",
					error_phase: "syntax",
					error_code: "jcs_error",
				}) + "\n",
			);
		}
		return;
	}

	if (!engine.ok) {
		process.stdout.write(
			JSON.stringify({
				version: PROTOCOL_VERSION,
				case_id: caseId,
				subject: "node-yaml",
				subject_version: subjectVersion(),
				status: "unsupported",
				error_phase: "driver",
				error_code: "missing_js_yaml",
			}) + "\n",
		);
		return;
	}
	try {
		const docs = engine.api.loadAll(text);
		const normDocs = (docs.length === 0 ? [null] : docs).map((d) =>
			toNotation(d),
		);
		const canonical = canonicalizeJCSValue(normDocs);
		const canonicalBytes = Buffer.from(canonical, "utf8");
		const digest = crypto
			.createHash("sha256")
			.update(canonicalBytes)
			.digest("hex");
		process.stdout.write(
			JSON.stringify({
				version: PROTOCOL_VERSION,
				case_id: caseId,
				subject: "node-yaml",
				subject_version: subjectVersion(),
				status: "ok",
				canonical_value: canonical,
				output_sha256: digest,
				output_bytes: canonicalBytes.length,
				document_count: normDocs.length,
			}) + "\n",
		);
	} catch (err) {
		const msg = String(err);
		if (msg.includes("ERR_MODULE_NOT_FOUND")) {
			process.stdout.write(
				JSON.stringify({
					version: PROTOCOL_VERSION,
					case_id: caseId,
					subject: "node-yaml",
					subject_version: subjectVersion(),
					status: "unsupported",
					error_phase: "driver",
					error_code: "missing_js_yaml",
				}) + "\n",
			);
			return;
		}
		process.stdout.write(
			JSON.stringify({
				version: PROTOCOL_VERSION,
				case_id: caseId,
				subject: "node-yaml",
				subject_version: subjectVersion(),
				status: req.budgets ? "refused_bounded" : "error",
				error_phase: "syntax",
				error_code: "js_yaml_error",
			}) + "\n",
		);
	}
}

main();
