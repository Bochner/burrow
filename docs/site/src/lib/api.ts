// The only operation data comes from the declared production binary build.
import inventory from "../../api-inventory.json";

const escape = (value: unknown) => String(value).replace(/[&<>"']/g, (char) => ({
  "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
}[char]!));
const code = (value: unknown) => `<code>${escape(value)}</code>`;
const json = (value: unknown) => `<pre><code>${escape(JSON.stringify(value, null, 2))}</code></pre>`;
const source = (path: string) => `<a href="https://github.com/Bochner/burrow/blob/main/${escape(path)}">${code(path)}</a>`;
const table = (headers: string[], rows: string[][]) => `<table><thead><tr>${headers.map((h) => `<th>${h}</th>`).join("")}</tr></thead><tbody>${rows.map((row) => `<tr>${row.map((cell) => `<td>${cell}</td>`).join("")}</tr>`).join("")}</tbody></table>`;
const fragment = (title: string, group: string, order: number, body: string) => `<!-- burrow-doc: ${JSON.stringify({ title, group, order })} -->\n<article><h1>${escape(title)}</h1>${body}</article>`;
const categories = [...new Set(inventory.operations.map((op) => op.category))].sort();
const invocation = (tokens: string[]) => tokens.map((token) => /^[a-zA-Z0-9_./:@=-]+$/.test(token) ? token : `'${token.replace(/'/g, "'\\''")}'`).join(" ");

export const apiFragments: Record<string, string> = {};
for (const [index, category] of categories.entries()) {
  const ops = inventory.operations.filter((op) => op.category === category);
  const title = `${category[0].toUpperCase()}${category.slice(1)} operations`;
  apiFragments[`../content/api/${category}.html`] = fragment(title, "Operations", index,
    `<p>Current ${escape(category)} routes from the production binary. Unsupported and terminal-only routes remain visible. Global flags precede the command; examples use existing defaults and omit approval.</p>` +
    table(["Capability", "Agent route", "Purpose"], ops.map((op) => [
      `<a href="#${escape(op.id)}">${code(op.id)}</a>`, escape(op.agent.status), escape(op.summary),
    ])) + ops.map((op) => `<section id="${escape(op.id)}"><h2>${code(op.id)}</h2><p>${escape(op.summary)}</p>` +
      table(["Contract", "Current behavior"], [
        ["Agent availability", `${escape(op.agent.status)}${op.agent.limitation ? ` — ${escape(op.agent.limitation)}` : ""}`],
        ["CLI / delegated call", [op.agent.syntax, ...(op.agent.variants ?? [])].map(code).join("<br>")], ["Human entry point", code(op.human)],
        ["Scope", escape(op.scope)], ["Effects / initialization", escape(op.effects)], ["Review", escape(op.review)],
        ["Inputs", op.inputs.length ? op.inputs.map((name) => `<a href="inputs.html#${escape(name)}">${code(name)}</a>`).join(", ") : "No operation-specific inputs; see invocation and global flags."],
        ["Result", `<a href="results.html#${escape(op.result)}">${code(op.result)}</a>; <a href="contract.html#errors">error and exit behavior</a>`],
        ["Base-module command", op.moduleCommand ? "Accepted through the verified workspace daemon; normal Hovel approval still applies." : "Unsupported as a base-module command string; use the route above."],
        ["Evidence", op.evidence.map((e) => `${escape(e.kind)}: ${source(e.source)}`).join("<br>")],
      ]) + (op.agent.example.length ? `<h3>Example</h3><pre><code>${escape(invocation(op.agent.example))}</code></pre>` : "") +
      `<p><a href="inventory.json">Machine-readable inventory</a> · <a href="contract.html#evidence">What this evidence proves</a></p></section>`).join(""));
}

apiFragments["../content/api/inputs.html"] = fragment("Inputs and defaults", "Contract", 10,
  `<p>Square brackets in an invocation mark optional inputs; unbracketed inputs are required. Option groups below document each mode. Normal calls use the shipped defaults; budget and timeout flags are optional overrides. These are documentation schemas, not a replacement for command validation.</p>` +
  `<h2 id="global-flags">Global flags</h2><p>Place these before the command. ${code("--help")} exits without setup. Discovery ignores setup flags and never initializes state.</p>` +
  table(["Flag", "Default", "Meaning"], inventory.globalFlags.map((f) => [code(f.name), code(f.default || "unset"), escape(f.description)])) +
  Object.entries(inventory.inputs).map(([name, shape]) => `<section id="${escape(name)}"><h2>${code(name)}</h2>${json(shape)}</section>`).join(""));

apiFragments["../content/api/results.html"] = fragment("Results and wire shapes", "Contract", 20,
  `<p>Named Go result shapes are derived from the actual JSON types. Union and map replies have explicit descriptions. These shapes document successful responses and reviews; they do not certify remote success. Fields such as ${code("remoteExit")}, ${code("localExit")}, ${code("outputComplete")}, ${code("cancellation")} and ${code("collection")} answer different questions.</p>` +
  Object.entries(inventory.results).sort(([a], [b]) => a.localeCompare(b)).map(([name, shape]) => `<section id="${escape(name)}"><h2>${code(name)}</h2>${json(shape)}</section>`).join(""));

apiFragments["../content/api/contract.html"] = fragment("Integration, provenance and evidence", "Contract", 30,
  `<p>Burrow exposes a CLI and one base Hovel module. It does not publish a separate REST service or SDK. This reference is generated with the same build as the site; use ${code("burrow capabilities")} to discover the contract of your installed executable.</p>` +
  `<h2 id="provenance">Version and source</h2>${json({ schemaVersion: inventory.schemaVersion, module: inventory.module, ...inventory.provenance })}` +
  `<h2 id="module">Base-module configuration</h2><p>${escape(inventory.integration.module)}</p><p>${escape(inventory.integration.command)}</p><p>${escape(inventory.integration.adapters)}</p>${json(inventory.moduleSchema)}` +
  `<h3>Saved-chain example</h3><p>Run ${code("burrow --workspace /absolute/workspace status")} once to establish the verified workspace. Save this Hovel chain as ${code("/absolute/workspace/list.chain.json")}; it inspects retained runs. Use the installed pinned Hovel CLI to review and confirm the throw. Do not add bypass flags to automation implicitly.</p>` + json({ apiVersion: "hovel.dev/v1alpha1", kind: "Chain", metadata: { name: "burrow-inspect" }, spec: { mode: "configured", steps: [{ id: "burrow", uses: "module:burrow@0.1.0" }], targets: [{ id: "local://burrow" }], config: { workspace: "/absolute/workspace", command: "run list" } } }) +
  `<pre><code>hovel throw /absolute/workspace/list.chain.json --workspace /absolute/workspace --daemon-endpoint /absolute/workspace/hoveld.sock --allow-dangerous --json</code></pre>` +
  `<h2 id="upstream">Authoritative Hovel boundaries</h2><p>${escape(inventory.integration.daemon)}</p>` +
  table(["Upstream contract", "Pinned source"], Object.entries(inventory.integration.upstream).map(([name, url]) => [escape(name), `<a href="${escape(url)}">${escape(name)} at the pinned Hovel SDK revision</a>`])) +
  `<h2 id="errors">Errors and exits</h2>${json(inventory.errors)}<p>${escape(inventory.integration.errors)}</p>` +
  `<h2 id="presentation">Aliases and presentation</h2>${table(["Alias / presentation route", "Capability"], Object.entries(inventory.presentation.aliases).map(([name, value]) => [code(name), escape(value)]))}<p>${escape(inventory.presentation.controls)}</p><p>${escape(inventory.presentation.stdout)}</p>` +
  `<h2 id="evidence">Evidence boundaries</h2><p>${escape(inventory.evidencePolicy)}</p>` +
  table(["Check", "What it demonstrates"], [
    [source("core/cmd/burrow/capabilities_check.py"), "Real executable discovery without local initialization; known route availability, ID lookup, schema references and command reachability."],
    [source("core/cmd/burrow/terminal_check.py"), "Real human PTY entry and headless CLI reach the same saved-profile behavior; a saved setting does not create a connection."],
    [source("docs/tools/docs/site_test.py"), "Built API inventory matches the binary contract, every capability has a deep link, navigation works, and search includes each ID."],
    ["Per-operation semantic-check pointers", "Focused existing assertions for selected actual behavior; read their scope. A pointer is not a claim that every possible outcome is tested."],
  ]) + `<p>Run ${code("aspect test //core/cmd/burrow:capabilities_test")}, ${code("aspect burrow-site check")} and the relevant ${code("aspect burrow-check")} gate. Route discovery and documentation do not establish full human/agent parity. The known upstream Hovel runtime limitation remains tracked in <a href="https://github.com/Bochner/burrow/issues/86">#86</a>.</p>`);

apiFragments["../content/api/index.html"] = fragment("Burrow API reference", "Overview", 0,
  `<p>Discover supported operations, their inputs, results, approval requirements and current gaps through the production CLI contract. This is Burrow's CLI and Hovel integration reference.</p>` +
  `<pre><code>burrow capabilities
burrow capabilities run.output</code></pre>` +
  `<p>Both calls emit JSON without a workspace, daemon, network connection or terminal UI. Operational commands require an explicit workspace. ${code("burrow --workspace /absolute/workspace status")} initializes local state; it is not a read-only probe.</p>` +
  table(["Reference", "Content"], categories.map((category) => [`<a href="${escape(category)}.html">${escape(category)}</a>`, "CLI and human routes, including unsupported routes"]).concat([
    ['<a href="inputs.html">Inputs and defaults</a>', "Arguments, optional flags and existing defaults"],
    ['<a href="results.html">Results</a>', "Structured wire shapes and review unions"],
    ['<a href="contract.html">Integration and provenance</a>', "Base-module configuration, upstream RPC/SDK, aliases, errors and evidence"],
    ['<a href="inventory.json">Download JSON inventory</a>', "The exact machine-readable contract used to build these pages"],
  ])) + `<p>Use search for a capability ID, CLI spelling or topic. Every capability has a stable fragment link. This inventory records current support; it makes no numerical parity claim. For worked operator walkthroughs, see the <a href="../spec/">Book</a>.</p>`);

export { inventory };
