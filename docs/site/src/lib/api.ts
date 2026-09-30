// The only operation data comes from the declared production binary build.
import inventory from "../../api-inventory.json";

const escape = (value: unknown) => String(value).replace(/[&<>"']/g, (char) => ({
  "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
}[char]!));
const code = (value: unknown) => `<code>${escape(value)}</code>`;
const json = (value: unknown) => `<pre data-lang="JSON"><code>${escape(JSON.stringify(value, null, 2))}</code></pre>`;
const source = (path: string) => `<a href="https://github.com/Bochner/burrow/blob/main/${escape(path)}">${code(path)}</a>`;
const table = (headers: string[], rows: string[][]) => `<table><thead><tr>${headers.map((h) => `<th>${h}</th>`).join("")}</tr></thead><tbody>${rows.map((row) => `<tr>${row.map((cell) => `<td>${cell}</td>`).join("")}</tr>`).join("")}</tbody></table>`;

interface Schema {
  $ref?: string;
  type?: string | string[];
  description?: string;
  default?: unknown;
  enum?: unknown[];
  const?: unknown;
  minimum?: number;
  maximum?: number;
  pattern?: string;
  properties?: Record<string, Schema>;
  required?: string[];
  items?: Schema;
  anyOf?: Schema[];
}
const typeName = (shape: Schema): string => shape.anyOf
  ? shape.anyOf.map(typeName).join(" | ")
  : shape.$ref ? shape.$ref.split("/").pop()!
  : shape.const !== undefined ? JSON.stringify(shape.const)
  : shape.enum ? shape.enum.map((value) => JSON.stringify(value)).join(" | ")
  : Array.isArray(shape.type) ? shape.type.map((type) => typeName({ ...shape, type })).join(" | ")
  : shape.type === "array" ? `${shape.items ? typeName(shape.items) : "unknown"}[]`
  : shape.type || "see schema";
const constraints = (shape: Schema): string => [
  shape.default !== undefined ? `Default: ${code(JSON.stringify(shape.default))}` : "",
  shape.enum ? `Allowed: ${shape.enum.map(code).join(", ")}` : "",
  shape.minimum !== undefined ? `Minimum: ${code(shape.minimum)}` : "",
  shape.maximum !== undefined ? `Maximum: ${code(shape.maximum)}` : "",
  shape.pattern ? `Pattern: ${code(shape.pattern)}` : "",
].filter(Boolean).join(" · ");
const fields = (shape: Schema): string => (shape.description ? `<p>${escape(shape.description)}</p>` : "") + (shape.properties
  ? table(["Field / option", "Type", "Presence", "Description / constraints"],
      Object.entries(shape.properties).map(([name, field]) => [code(name), code(typeName(field)),
        shape.required?.includes(name) ? "Required" : "Optional",
        [escape(field.description || ""), constraints(field)].filter(Boolean).join("<br>") || "See full schema."]))
  : `<p>${code(typeName(shape))}${constraints(shape) ? ` · ${constraints(shape)}` : ""}</p>`);
const schema = (shape: Schema) => fields(shape) + `<details><summary>Full JSON schema</summary>${json(shape)}</details>`;
const parameters = (names: string[]) => names.length
  ? `<dl class="api-parameters">${names.map((name) => {
      const shape = inventory.inputs[name as keyof typeof inventory.inputs] as Schema;
      return `<div><dt><a href="inputs.html#${escape(name)}">${code(name)}</a> · ${code(typeName(shape))}</dt>` +
        `<dd>${escape(shape.description || "")}${constraints(shape) ? `<p>${constraints(shape)}</p>` : ""}` +
        (shape.properties ? `<details><summary>Options / request fields</summary>${fields(shape)}</details>` : "") + "</dd></div>";
    }).join("")}</dl>`
  : "<p>No operation-specific parameters.</p>";

const fragment = (title: string, group: string, order: number, body: string) => `<!-- burrow-doc: ${JSON.stringify({ title, group, order })} -->\n<article><h1>${escape(title)}</h1>${body}</article>`;
const categories = [...new Set(inventory.operations.map((op) => op.category))].sort();
const invocation = (tokens: string[]) => tokens.map((token) => /^[a-zA-Z0-9_./:@=-]+$/.test(token) ? token : `'${token.replace(/'/g, "'\\''")}'`).join(" ");

export const apiFragments: Record<string, string> = {};
for (const [index, category] of categories.entries()) {
  const ops = inventory.operations.filter((op) => op.category === category);
  const title = `${category[0].toUpperCase()}${category.slice(1)} operations`;
  apiFragments[`../content/api/${category}.html`] = fragment(title, "Operations", index,
    `<p>Commands and contracts for ${escape(category)}. Place global flags before the command. Examples use placeholders and do not grant approval.</p>` +
    table(["Operation", "Availability", "Purpose"], ops.map((op) => [
      `<a href="#${escape(op.id)}">${code(op.id)}</a>`, escape(op.agent.status), escape(op.summary),
    ])) + ops.map((op) => `<section class="api-operation" id="${escape(op.id)}"><h2>${code(op.id)}</h2><p>${escape(op.summary)}</p>` +
      `<p><strong>${escape(op.agent.status)}</strong>${op.agent.limitation ? ` — ${escape(op.agent.limitation)}` : ""}</p>` +
      `<h3>Invocation</h3><pre data-lang="CLI syntax"><code>${escape([op.agent.syntax, ...(op.agent.variants ?? [])].filter(Boolean).join("\n"))}</code></pre>` +
      (op.agent.example.length ? `<h3>Example</h3><pre data-lang="Local shell"><code>${escape(invocation(op.agent.example))}</code></pre>` : "") +
      `<h3>Parameters</h3>${parameters(op.inputs)}` +
      `<h3>Approval and effects</h3><p>${escape(op.review)}</p><p>${escape(op.effects)}</p>` +
      `<h3>Returns</h3><p><a href="results.html#${escape(op.result)}">${code(op.result)}</a>. See <a href="contract.html#errors">errors and exit status</a> before interpreting command success.</p>` +
      `<details><summary>Other entry points and verification</summary>` + table(["Contract", "Behavior"], [
        ["Human entry point", code(op.human)], ["Scope", escape(op.scope)],
        ["Headless equivalents", ((op.agent as { equivalents?: string[] }).equivalents ?? []).map((id) => `<a href="${escape(inventory.operations.find((entry) => entry.id === id)!.category)}.html#${escape(id)}">${code(id)}</a>`).join(", ") || "Use the direct invocation or availability limitation above."],
        ["Base-module command", op.moduleCommand ? "Accepted through the verified daemon with normal Hovel approval." : "Use the advertised route above; no base-module command string."],
        ["Presentation", escape((op as { presentationOnly?: string }).presentationOnly || "Includes operational behavior.")],
        ["Source / checks", op.evidence.map((e) => `${escape(e.kind)}: ${source(e.source)}`).join("<br>")],
      ]) + `<p><a href="contract.html#evidence">Evidence limits</a> · <a href="inventory.json">JSON inventory</a></p></details></section>`).join(""));
}

apiFragments["../content/api/inputs.html"] = fragment("Inputs and defaults", "Contract", 10,
  `<p>Square brackets in an invocation mark optional inputs; unbracketed inputs are required. Option groups below document each mode. Normal calls use the shipped defaults; budget and timeout flags are optional overrides. These are documentation schemas, not a replacement for command validation.</p>` +
  `<h2 id="global-flags">Global flags</h2><p>Place these before the command. ${code("--help")} exits without setup. Discovery ignores setup flags and never initializes state.</p>` +
  table(["Flag", "Default", "Meaning"], inventory.globalFlags.map((f) => [code(f.name), code(f.default || "unset"), escape(f.description)])) +
  Object.entries(inventory.inputs).map(([name, shape]) => `<section id="${escape(name)}"><h2>${code(name)}</h2>${schema(shape)}</section>`).join(""));

apiFragments["../content/api/results.html"] = fragment("Results and wire shapes", "Contract", 20,
  `<p>Named Go result shapes are derived from the actual JSON types. Union and map replies have explicit descriptions. These shapes document successful responses and reviews; they do not certify remote success. Fields such as ${code("remoteExit")}, ${code("localExit")}, ${code("outputComplete")}, ${code("cancellation")} and ${code("collection")} answer different questions.</p>` +
  Object.entries(inventory.results).sort(([a], [b]) => a.localeCompare(b)).map(([name, shape]) => `<section id="${escape(name)}"><h2>${code(name)}</h2>${schema(shape)}</section>`).join(""));

apiFragments["../content/api/contract.html"] = fragment("Integration, provenance and evidence", "Contract", 30,
  `<p>Burrow exposes a CLI and one base Hovel module. It does not publish a separate REST service or SDK. This reference is generated with the same build as the site; use ${code("burrow capabilities")} to discover the contract of your installed executable.</p>` +
  `<h2 id="provenance">Version and source</h2><details><summary>Build provenance</summary>${json({ schemaVersion: inventory.schemaVersion, module: inventory.module, ...inventory.provenance })}</details>` +
  `<h2 id="module">Base-module configuration</h2><p>${escape(inventory.integration.module)}</p><p>${escape(inventory.integration.command)}</p><p>${escape(inventory.integration.adapters)}</p>${json(inventory.moduleSchema)}` +
  `<h3>Saved-chain example</h3><p>Run ${code("burrow --workspace /absolute/workspace status")} once to establish the verified workspace. Save this Hovel chain as ${code("/absolute/workspace/list.chain.json")}; it inspects retained runs. Use the installed pinned Hovel CLI to review and confirm the throw. Do not add bypass flags to automation implicitly.</p>` + json({ apiVersion: "hovel.dev/v1alpha1", kind: "Chain", metadata: { name: "burrow-inspect" }, spec: { mode: "configured", steps: [{ id: "burrow", uses: "module:burrow@0.1.0" }], targets: [{ id: "local://burrow" }], config: { workspace: "/absolute/workspace", command: "run list" } } }) +
  `<pre data-lang="Local shell"><code>hovel throw /absolute/workspace/list.chain.json --workspace /absolute/workspace --daemon-endpoint /absolute/workspace/hoveld.sock --allow-dangerous --json</code></pre>` +
  `<h2 id="upstream">Authoritative Hovel boundaries</h2><p>${escape(inventory.integration.daemon)}</p>` +
  table(["Upstream contract", "Pinned source"], Object.entries(inventory.integration.upstream).map(([name, url]) => [escape(name), `<a href="${escape(url)}">${escape(name)} at the pinned Hovel SDK revision</a>`])) +
  `<h2 id="errors">Errors and exit status</h2><p>Successful calls and passive reviews exit 0. A successful CLI call can still describe a failed remote command, partial transfer or incomplete capture. Check the returned outcome fields.</p>${table(["Route", "Error output", "Exit"], [
    ["workspace subcommands", "JSON on stderr; workspace-specific codes", "1"],
    ["close, profile connect, session commands", "JSON on stderr; operation_failed", "1"],
    ["other operations", "Text on stderr", "1"],
    ["global flag parsing", "Text; see the installed CLI", "nonzero"],
  ])}<p>Missing acknowledgement does not promise rollback. Inspect uncertain state before retrying.</p><details><summary>Exact error contract</summary>${json(inventory.errors)}<p>${escape(inventory.integration.errors)}</p></details>` +
  `<h2 id="presentation">Aliases and presentation</h2>${table(["Alias / presentation route", "Capability"], Object.entries(inventory.presentation.aliases).map(([name, value]) => [code(name), escape(value)]))}<p>${escape(inventory.presentation.controls)}</p><p>${escape(inventory.presentation.stdout)}</p>` +
  `<h2 id="evidence">Evidence boundaries</h2><p>${escape(inventory.evidencePolicy)}</p>` +
  table(["Check", "What it demonstrates"], [
    [source("core/cmd/burrow/capabilities_check.py"), "Real executable discovery without local initialization; known route availability, ID lookup, schema references and command reachability."],
    [source("core/cmd/burrow/terminal_check.py"), "Real human PTY entry and headless CLI reach the same saved-profile behavior; a saved setting does not create a connection."],
    [source("docs/tools/docs/site_test.py"), "Built API inventory matches the binary contract, every capability has a deep link, navigation works, and search includes each ID."],
    ["Per-operation semantic-check pointers", "Focused existing assertions for selected actual behavior; read their scope. A pointer is not a claim that every possible outcome is tested."],
  ]) + `<p>Run ${code("aspect test //core/cmd/burrow:capabilities_test")}, ${code("aspect burrow-site check")} and the relevant ${code("aspect burrow-check")} gate. Route discovery and documentation do not establish full human/agent parity. The pinned Hovel v0.4.4 compatibility checks are required in the full gate.</p>`);

apiFragments["../content/api/index.html"] = fragment("Burrow API reference", "Overview", 0,
  `<p>Reference for Burrow's CLI and base Hovel module: command syntax, parameters, result fields, approval and effects. The pages are generated from the production binary.</p>` +
  `<pre data-lang="Local shell"><code>burrow capabilities
burrow capabilities run.output</code></pre>` +
  `<p>Discovery emits JSON without a workspace or daemon. For operations, select an explicit workspace; <a href="workspace.html#workspace.open">workspace open</a> initializes state, while <a href="workspace.html#workspace.inspect">workspace inspect</a> only verifies an existing daemon.</p>` +
  `<h2>Operations</h2><div class="tiles">${categories.map((category) => `<a class="tile" href="${escape(category)}.html"><span class="tile-label">CLI / Hovel</span><span class="tile-title">${escape(category[0].toUpperCase() + category.slice(1))}</span><span class="tile-desc">${inventory.operations.filter((op) => op.category === category).length} documented operations</span></a>`).join("")}</div>` +
  `<h2>Shared contracts</h2>` + table(["Reference", "Content"], [
    ['<a href="inputs.html">Parameters and defaults</a>', "Types, required fields, options and bounds"],
    ['<a href="results.html">Return types</a>', "Structured result fields and complete JSON schemas"],
    ['<a href="contract.html">Hovel integration</a>', "Module configuration, errors, upstream RPC/SDK and provenance"],
    ['<a href="inventory.json">Download JSON inventory</a>', "The exact contract used to generate these pages"],
  ]) + `<p>Burrow exposes one Hovel module and no separate REST service or SDK. Search by operation ID or CLI spelling; each operation has a stable deep link. Use the <a href="../spec/">Book</a> for worked workflows.</p>`);

export { inventory };
