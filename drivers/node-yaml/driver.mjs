#!/usr/bin/env node
// Standalone Node.js subject driver implementing docpresser-driver/v0.

import crypto from "node:crypto";
import fs from "node:fs";

const PROTOCOL_VERSION = "docpresser-driver/v0";

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
	const args = process.argv.slice(2);
	if (args.includes("--docpresser-handshake")) {
		const resp = {
			version: PROTOCOL_VERSION,
			subject: "node-yaml",
			subject_version: process.version,
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
					subject_version: process.version,
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
					subject_version: process.version,
					status: "error",
					error_phase: "syntax",
					error_code: "jcs_error",
				}) + "\n",
			);
		}
		return;
	}

	try {
		const jsYaml = await import("js-yaml");
		const docs = jsYaml.loadAll(text);
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
				subject_version: process.version,
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
					subject_version: process.version,
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
				subject_version: process.version,
				status: req.budgets ? "refused_bounded" : "error",
				error_phase: "syntax",
				error_code: "js_yaml_error",
			}) + "\n",
		);
	}
}

main();
