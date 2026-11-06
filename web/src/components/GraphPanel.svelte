<script lang="ts">
  import { app, nodeMatches, openFile, saveList, savePref, saveStr, selectNode } from '../lib/store.svelte';
  import { STEREOTYPE, stereo } from '../lib/theme';
  import { dependencies, dependents, path, type Metrics } from '../lib/graphAnalysis';
  import Glyph from './Glyph.svelte';

  let open = $state<Record<string, boolean>>({ inspect: true, find: true, view: true, roles: true, services: true, relations: true, packages: false, analysis: true, hotspots: false });
  const all = $derived(app.graph?.nodes ?? []);
  const visible = $derived(app.graphView);
  const metrics = $derived(app.graphMetrics as Map<string, Metrics>);
  const cycles = $derived(app.graphCycles);
  const violations = $derived(app.graphViolations);
  const byId = $derived(new Map(all.map((n) => [n.id, n])));
  const name = (id: string) => byId.get(id)?.name ?? id.slice(id.lastIndexOf('.') + 1);
  // Lists where names sit apart from the graph need the module to tell same-named classes apart.
  const clashes = $derived.by(() => {
    const seen = new Map<string, number>();
    for (const n of all) seen.set(n.name, (seen.get(n.name) ?? 0) + 1);
    return seen;
  });
  const label = (id: string) => {
    const n = byId.get(id);
    return n?.module && (clashes.get(n.name) ?? 0) > 1 ? `${n.name} (${n.module})` : name(id);
  };
  const count = (key: (n: (typeof all)[number]) => string) => {
    const c = new Map<string, number>();
    for (const n of all) c.set(key(n), (c.get(key(n)) ?? 0) + 1);
    return c;
  };
  const roles = $derived.by(() => {
    const c = count((n) => n.stereotype);
    return Object.keys(STEREOTYPE).filter((k) => c.has(k)).map((k) => ({ key: k, n: c.get(k)! }));
  });
  const pkgs = $derived([...count((n) => n.pkg)].sort((a, b) => a[0].localeCompare(b[0])));
  const modules = $derived([...count((n) => n.module ?? '')].filter(([m]) => m).sort((a, b) => a[0].localeCompare(b[0])));
  const edgeKinds = $derived([...new Set((app.graph?.edges ?? []).map((e) => e.kind))]);
  const edgeText: Record<string, string> = { injects: 'injects: a field or constructor dependency', uses: 'uses: calls, parameters, return types', feign: 'feign: an HTTP call to another service', extends: 'extends', implements: 'implements' };
  const q = $derived(app.graphQuery.trim().toLowerCase());
  const hits = $derived(q ? all.filter((n) => nodeMatches(n, q)) : []);

  type ListName = 'hiddenRoles' | 'hiddenPkgs' | 'hiddenEdges' | 'hiddenNodes' | 'hiddenModules';
  function toggle(list: ListName, v: string) {
    const cur = new Set(app[list]);
    if (cur.has(v)) cur.delete(v);
    else cur.add(v);
    app[list] = [...cur];
    saveList(list, app[list]);
  }
  function only(list: ListName, keep: string, values: string[]) {
    app[list] = values.filter((v) => v !== keep);
    saveList(list, app[list]);
  }
  function showAll() {
    for (const l of ['hiddenRoles', 'hiddenPkgs', 'hiddenEdges', 'hiddenNodes', 'hiddenModules'] as const) { app[l] = []; saveList(l, []); }
    app.graphQuery = '';
    requestAnimationFrame(() => window.dispatchEvent(new Event('codist:relayout')));
  }
  function setLayout(v: typeof app.layoutMode) { app.layoutMode = v; saveStr('layoutMode', v); }
  function setSize(v: typeof app.sizeBy) { app.sizeBy = v; saveStr('sizeBy', v); }
  function setGroup(v: typeof app.groupBy) { app.groupBy = v; saveStr('groupBy', v); }
  function focusNode(id: string) {
    selectNode(id);
    window.dispatchEvent(new CustomEvent('codist:center', { detail: id }));
  }
  function find(e: KeyboardEvent) {
    if (e.key === 'Enter' && hits[0]) focusNode(hits[0].id);
    if (e.key === 'Escape') app.graphQuery = '';
  }

  // The class being inspected.
  const sel = $derived(app.selectedNode ? byId.get(app.selectedNode) ?? null : null);
  const selM = $derived(sel ? metrics.get(sel.id) : null);
  const uses = $derived(sel && app.graph ? app.graph.edges.filter((e) => e.source === sel.id) : []);
  const usedBy = $derived(sel && app.graph ? app.graph.edges.filter((e) => e.target === sel.id) : []);
  const constants = $derived(sel?.members.filter((m) => m.value) ?? []);
  const fields = $derived(sel?.members.filter((m) => m.kind === 'field' && !m.value) ?? []);
  const methods = $derived(sel?.members.filter((m) => m.kind !== 'field') ?? []);
  const issuesHere = $derived(sel ? (app.issueList?.issues ?? []).filter((i) => i.state === 'open' && i.path === sel.file) : []);
  const go = (line: number) => { if (sel) { app.showPanel = true; void openFile(sel.file, null, line); } };
  function expand() {
    if (!sel) return;
    const cur = new Set(app.expanded);
    if (cur.has(sel.id)) cur.delete(sel.id);
    else cur.add(sel.id);
    app.expanded = [...cur];
  }
  const costly = (c?: { loopIo: number; loopDepth: number; complexity: number }) => !!c && (c.loopIo > 0 || c.loopDepth > 1 || c.complexity > 10);

  const blast = $derived(app.overlay === 'blast' && app.overlayNode && app.graph ? [...dependents(app.graph, app.overlayNode)].sort((a, b) => a[1] - b[1]) : []);
  const route = $derived(app.overlay === 'path' && app.pathFrom && app.pathTo && app.graph ? path({ ...app.graph, edges: visible.edges }, app.pathFrom, app.pathTo) : []);
  const reverseRoute = $derived(app.overlay === 'path' && app.pathFrom && app.pathTo && app.graph && !route.length ? path({ ...app.graph, edges: visible.edges }, app.pathTo, app.pathFrom) : []);
  const reach = $derived(sel && app.graph ? { out: dependencies(app.graph, sel.id).size, in: dependents(app.graph, sel.id).size } : null);

  const top = (f: (m: Metrics, id: string) => number, n = 5) =>
    visible.nodes.map((x) => ({ id: x.id, v: f(metrics.get(x.id)!, x.id) })).filter((x) => x.v > 0).sort((a, b) => b.v - a.v).slice(0, n);
  const mostUsed = $derived(top((m) => m.fanIn));
  const fragile = $derived(top((m) => (m.fanIn >= 1 && m.fanOut >= 2 ? m.instability * m.fanIn : 0)));
  const largest = $derived(top((m) => m.lines));
  const sorted = $derived([...visible.nodes].sort((a, b) => a.name.localeCompare(b.name)));

  let analysis = $state<HTMLElement>();
  $effect(() => {
    if (app.overlay === 'none') return;
    open.analysis = true;
    requestAnimationFrame(() => analysis?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }));
  });
  const anyHidden = $derived(app.hiddenRoles.length || app.hiddenPkgs.length || app.hiddenNodes.length || app.hiddenEdges.length || app.hiddenModules.length || q);
</script>

{#snippet head(key: string, title: string, extra?: string)}
  <button class="sh" aria-expanded={open[key]} onclick={() => (open[key] = !open[key])}>
    <svg class:closed={!open[key]} width="10" height="10" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2.4"><path d="m3 6 5 5 5-5"/></svg>{title}{#if extra}<span class="x">{extra}</span>{/if}
  </button>
{/snippet}

<aside class="explorer" aria-label="Explorer">
  <header><span class="faint">{visible.nodes.length} of {all.length} classes · {visible.edges.length} links</span></header>

  {#if sel}
    <section>
      {@render head('inspect', 'Selected class')}
      {#if open.inspect}
        <div class="title"><Glyph stereotype={sel.stereotype} /><b>{sel.name}</b><button class="close" aria-label="Clear selection" onclick={() => selectNode(null)}>×</button></div>
        <div class="faint small mono">{sel.module ? `${sel.module} · ` : ''}{sel.pkg}</div>
        {#if sel.feignTarget}<div class="small feign">Feign client → {sel.feignTarget}</div>{/if}
        <div class="nums">
          <span title="Classes that depend on it"><b>{selM?.fanIn}</b> in</span><span title="Classes it depends on"><b>{selM?.fanOut}</b> out</span>
          <span title="Everything that may break if it changes"><b>{reach?.in}</b> blast</span><span><b>{selM?.lines}</b> lines</span>
          {#if issuesHere.length}<span class="bad"><b>{issuesHere.length}</b> issues</span>{/if}
        </div>
        <div class="acts">
          <button class="btn secondary sm" onclick={() => go(sel.startLine)}>Open code</button>
          <button class="btn secondary sm" onclick={expand}>{app.expanded.includes(sel.id) ? 'Collapse on graph' : 'Expand on graph'}</button>
          <button class="btn secondary sm" onclick={() => { app.overlay = 'blast'; app.overlayNode = sel.id; }}>Blast radius</button>
        </div>
        {#if sel.endpoints.length}
          <div class="label">Endpoints · {sel.endpoints.length}</div>
          {#each sel.endpoints as e}<div class="mem mono ep">{e}</div>{/each}
        {/if}
        {#if constants.length}
          <div class="label">Constants · {constants.length}</div>
          {#each constants as m}<button class="mem" onclick={() => go(m.startLine)}><span class="ic k">K</span><span class="mono nm">{m.name}</span><span class="mono v">{m.value}</span></button>{/each}
        {/if}
        {#if fields.length}
          <div class="label">Fields · {fields.length}</div>
          {#each fields as m}<button class="mem" title={m.signature} onclick={() => go(m.startLine)}><span class="ic f">f</span><span class="mono nm">{m.signature}</span></button>{/each}
        {/if}
        {#if methods.length}
          <div class="label">Methods · {methods.length} <span class="faint">with what each costs</span></div>
          {#each methods as m}
            <button class="mem" title={m.signature} onclick={() => go(m.startLine)}>
              <span class="ic m">m</span><span class="mono nm">{m.name}()</span>
              {#if m.cost}<span class="cost" class:hot={costly(m.cost)} title="Complexity {m.cost.complexity} · {m.cost.lines} lines · {m.cost.dbCalls} database and {m.cost.remoteCalls} remote calls, {m.cost.loopIo} in loops">{m.cost.estimate}{m.cost.complexity > 5 ? ` · c${m.cost.complexity}` : ''}</span>{/if}
            </button>
          {/each}
        {/if}
        {#if uses.length}
          <div class="label">Depends on · {uses.length}</div>
          <div class="names">{#each uses as e}<button class:fe={e.kind === 'feign'} title={e.via?.join(', ') ?? e.kind} onclick={() => focusNode(e.target)}>{label(e.target)}</button>{/each}</div>
        {/if}
        {#if usedBy.length}
          <div class="label">Used by · {usedBy.length}</div>
          <div class="names">{#each usedBy as e}<button class:fe={e.kind === 'feign'} title={e.via?.join(', ') ?? e.kind} onclick={() => focusNode(e.source)}>{label(e.source)}</button>{/each}</div>
        {/if}
      {/if}
    </section>
  {/if}

  <section>
    {@render head('find', 'Find')}
    {#if open.find}
      <input class="in" bind:value={app.graphQuery} onkeydown={find} placeholder="Class, method, constant, route, @Annotation…" spellcheck="false" />
      <div class="row">
        <div class="seg">
          <button class:on={app.graphQueryMode === 'highlight'} onclick={() => (app.graphQueryMode = 'highlight')}>Highlight</button>
          <button class:on={app.graphQueryMode === 'only'} onclick={() => (app.graphQueryMode = 'only')}>Only matches</button>
        </div>
        {#if q}<span class="faint small">{hits.length} · ↵ jumps</span>{/if}
      </div>
      {#if q && hits.length}
        <div class="list">{#each hits.slice(0, 8) as h}<button onclick={() => focusNode(h.id)}><Glyph stereotype={h.stereotype} />{h.name}<span class="faint mono">{h.module ? `${h.module} · ` : ''}{h.pkg.split('.').pop()}</span></button>{/each}</div>
      {/if}
    {/if}
  </section>

  <section>
    {@render head('view', 'View')}
    {#if open.view}
      <div class="label">Shape</div>
      <div class="grid3">
        {#each [['natural', 'Graph', 'Force-directed: related classes pull together. Drag anything.'], ['layers', 'Layers', 'Controllers, then services, repositories, entities'], ['services', 'Services', 'One node per service, with their calls']] as [v, t, d]}
          <button class:on={app.layoutMode === v} title={d} onclick={() => setLayout(v as typeof app.layoutMode)}>{t}</button>
        {/each}
      </div>
      <div class="label">Group by</div>
      <div class="seg wide">
        <button class:on={app.groupBy === 'none'} onclick={() => setGroup('none')}>Nothing</button>
        <button class:on={app.groupBy === 'package'} onclick={() => setGroup('package')}>Package</button>
        <button class:on={app.groupBy === 'service'} onclick={() => setGroup('service')}>Service</button>
      </div>
      {#if app.groupBy === 'service' && new Set(app.graphView.nodes.map((n) => n.module ?? '')).size <= 1}
        <p class="hint">This branch has one service, so the boxes are its top-level packages.</p>
      {/if}
      <div class="label">Size by</div>
      <div class="seg wide">
        <button class:on={app.sizeBy === 'links'} onclick={() => setSize('links')}>Links</button>
        <button class:on={app.sizeBy === 'lines'} onclick={() => setSize('lines')}>Code size</button>
        <button class:on={app.sizeBy === 'issues'} onclick={() => setSize('issues')}>Issues</button>
      </div>
      <div class="row wrap">
        <label class="tg"><input type="checkbox" bind:checked={app.showLabels} onchange={() => savePref('labels', app.showLabels)} /> Labels</label>
        <label class="tg" title="Dim what is not relevant to the selected commit or listed issues"><input type="checkbox" bind:checked={app.focusChanges} /> Focus</label>
        <label class="tg"><input type="checkbox" bind:checked={app.showLegend} /> Legend</label>
      </div>
      <div class="row wrap">
        <button class="btn secondary sm" onclick={() => window.dispatchEvent(new Event('codist:png'))}>Export PNG</button>
        <button class="btn secondary sm" onclick={() => (app.showGuide = true)}>What does it all mean?</button>
      </div>
    {/if}
  </section>

  <section>
    {@render head('roles', 'Categories', 'click to hide')}
    {#if open.roles}
      <div class="chips">
        {#each roles as r}
          <button class="chip-t" class:off={r.key === 'test' ? !app.showTests : app.hiddenRoles.includes(r.key)}
            onclick={() => { if (r.key === 'test') app.showTests = !app.showTests; else toggle('hiddenRoles', r.key); }}
            ondblclick={() => { if (r.key !== 'test') only('hiddenRoles', r.key, roles.map((x) => x.key)); }}>
            <Glyph stereotype={r.key} />{stereo(r.key).label}<span class="n">{r.n}</span>
          </button>
        {/each}
      </div>
    {/if}
  </section>

  {#if modules.length}
    <section>
      {@render head('services', 'Services', String(modules.length))}
      {#if open.services}
        <div class="pk">
          {#each modules as [m, n]}
            <label><input type="checkbox" checked={!app.hiddenModules.includes(m)} onchange={() => toggle('hiddenModules', m)} /><span class="mono">{m}</span><button class="only" onclick={() => only('hiddenModules', m, modules.map(([x]) => x))}>only</button><span class="faint">{n}</span></label>
          {/each}
        </div>
      {/if}
    </section>
  {/if}

  <section>
    {@render head('relations', 'Relations')}
    {#if open.relations}
      <div class="chips">
        {#each edgeKinds as k}
          <button class="chip-t" class:off={app.hiddenEdges.includes(k)} onclick={() => toggle('hiddenEdges', k)} title={edgeText[k] ?? k}><i class="ln {k}"></i>{k}</button>
        {/each}
      </div>
    {/if}
  </section>

  <section>
    {@render head('packages', 'Packages', String(pkgs.length))}
    {#if open.packages}
      <div class="pk">
        {#each pkgs as [p, n]}
          <label><input type="checkbox" checked={!app.hiddenPkgs.includes(p)} onchange={() => toggle('hiddenPkgs', p)} /><span class="mono">{p}</span><span class="faint">{n}</span></label>
        {/each}
      </div>
    {/if}
  </section>

  {#if anyHidden}
    <section class="reset">
      {#if app.hiddenNodes.length}
        <div class="label">Hidden classes · {app.hiddenNodes.length}</div>
        <div class="list">{#each app.hiddenNodes.slice(0, 8) as id}<button onclick={() => toggle('hiddenNodes', id)}>{label(id)}<span class="faint">show</span></button>{/each}</div>
      {/if}
      <button class="btn secondary sm" onclick={showAll}>Reset all filters</button>
    </section>
  {/if}

  <section bind:this={analysis}>
    {@render head('analysis', 'Analysis')}
    {#if open.analysis}
      <div class="seg wide">
        <button class:on={app.overlay === 'none'} onclick={() => (app.overlay = 'none')}>Off</button>
        <button class:on={app.overlay === 'cycles'} onclick={() => (app.overlay = 'cycles')} title="Classes that depend on each other in a loop">Cycles {cycles.length}</button>
        <button class:on={app.overlay === 'layers'} onclick={() => (app.overlay = 'layers')} title="Dependencies breaking controller → service → repository → entity">Layers {violations.length}</button>
        <button class:on={app.overlay === 'blast'} onclick={() => (app.overlay = 'blast')}>Blast</button>
        <button class:on={app.overlay === 'path'} onclick={() => (app.overlay = 'path')}>Path</button>
      </div>
      {#if app.overlay === 'cycles'}
        <p class="note">A loop of dependencies: none of these classes can change, be tested or be reused alone.</p>
        {#each cycles as c, i}
          <div class="card"><b>Cycle {i + 1}</b> · {c.length} classes<div class="names">{#each c as id}<button onclick={() => focusNode(id)}>{label(id)}</button>{/each}</div></div>
        {:else}<p class="good">No dependency cycles.</p>{/each}
      {:else if app.overlay === 'layers'}
        <p class="note">Arrows going up, or controllers reaching a repository directly, bypass business rules and transactions.</p>
        {#each violations as v}
          <div class="card"><button class="inl" onclick={() => focusNode(v.source)}>{label(v.source)}</button> → <button class="inl" onclick={() => focusNode(v.target)}>{label(v.target)}</button><div class="faint small">{v.why}</div></div>
        {:else}<p class="good">No layer violations.</p>{/each}
      {:else if app.overlay === 'blast'}
        <select class="in" bind:value={app.overlayNode}><option value={null}>Pick a class…</option>{#each sorted as n}<option value={n.id}>{label(n.id)}</option>{/each}</select>
        {#if app.overlayNode}
          <p class="note">If <b>{label(app.overlayNode)}</b> changes, these may break (darker = closer).</p>
          {#each [1, 2, 3] as d}
            {@const at = blast.filter(([, x]) => (d === 3 ? x >= 3 : x === d))}
            {#if at.length}<div class="label">{d === 1 ? 'Directly' : d === 2 ? 'Through one other' : 'Further away'} · {at.length}</div>
              <div class="names">{#each at as [id]}<button onclick={() => focusNode(id)}>{label(id)}</button>{/each}</div>{/if}
          {/each}
          {#if !blast.length}<p class="good">Nothing depends on it.</p>{/if}
        {/if}
      {:else if app.overlay === 'path'}
        <div class="row2">
          <select class="in" bind:value={app.pathFrom}><option value={null}>From…</option>{#each sorted as n}<option value={n.id}>{label(n.id)}</option>{/each}</select>
          <select class="in" bind:value={app.pathTo}><option value={null}>To…</option>{#each sorted as n}<option value={n.id}>{label(n.id)}</option>{/each}</select>
        </div>
        {#if app.pathFrom && app.pathTo}
          {#if route.length}<p class="note">{route.length - 1} step{route.length === 2 ? '' : 's'}: {#each route as id, i}{i ? ' → ' : ''}<button class="inl" onclick={() => focusNode(id)}>{label(id)}</button>{/each}</p>
          {:else if reverseRoute.length}<p class="note">No path that way, but {label(app.pathTo)} reaches {label(app.pathFrom)} in {reverseRoute.length - 1} steps.</p>
          {:else}<p class="note">Not connected with the current filters.</p>{/if}
        {/if}
      {/if}
    {/if}
  </section>

  <section>
    {@render head('hotspots', 'Hotspots')}
    {#if open.hotspots}
      <div class="label">Most depended on <span class="faint">changes here ripple</span></div>
      {#each mostUsed as h}<button class="hs" onclick={() => focusNode(h.id)}>{label(h.id)}<span class="faint">{h.v} in</span></button>{/each}
      <div class="label">Fragile <span class="faint">depends on much, and is depended on</span></div>
      {#each fragile as h}<button class="hs" onclick={() => focusNode(h.id)}>{label(h.id)}<span class="faint">in {metrics.get(h.id)?.fanIn} · out {metrics.get(h.id)?.fanOut}</span></button>{:else}<p class="faint small">None.</p>{/each}
      <div class="label">Largest</div>
      {#each largest as h}<button class="hs" onclick={() => focusNode(h.id)}>{label(h.id)}<span class="faint">{h.v} lines</span></button>{/each}
    {/if}
  </section>
</aside>

<style>
  .explorer { display: flex; flex-direction: column; min-height: 0; flex: 1; overflow: auto; background: var(--surface); font-size: 12.5px; }
  header { display: grid; gap: 1px; padding: 0 16px 8px; }
  header .faint { font-size: 11px; }
  section { margin: 0 10px 10px; padding: 10px 12px; border-radius: var(--r-lg); background: var(--surface-2); display: grid; gap: 8px; min-width: 0; }
  .sh { display: inline-flex; align-items: center; gap: 5px; justify-self: start; border: 0; background: none; padding: 0; font-size: 13px; font-weight: 600; color: var(--text); }
  .sh:hover { color: var(--text); }
  .sh svg { transition: transform 0.15s; }
  .sh svg.closed { transform: rotate(-90deg); }
  .sh .x { margin-left: 4px; font-weight: 400; text-transform: none; letter-spacing: 0; color: var(--text-3); }
  section :global(.btn) { justify-self: start; }
  .label { font-size: 11px; color: var(--text-2); font-weight: 500; }
  .label .faint { font-weight: 400; }
  .in { width: 100%; height: 28px; padding: 0 9px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12px; }
  .in:focus { outline: none; border-color: var(--accent); }
  .row { display: flex; align-items: center; gap: 8px; }
  .row.wrap { flex-wrap: wrap; }
  .row2 { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; }
  .seg { display: flex; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg.wide button { flex: 1; padding: 0 3px; }
  .seg button { height: 24px; padding: 0 8px; border: 0; border-radius: var(--r); background: none; font-size: 11.5px; color: var(--text-3); white-space: nowrap; }
  .seg button.on, .grid3 button.on { color: var(--text); background: var(--surface-3); box-shadow: var(--raised); }
  .grid3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: 2px; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .grid3 button { height: 24px; border: 0; border-radius: var(--r); background: none; font-size: 11.5px; color: var(--text-3); }
  .chips { display: flex; flex-wrap: wrap; gap: 4px; }
  .chip-t { display: inline-flex; align-items: center; gap: 5px; height: 24px; padding: 0 8px; border: 1px solid var(--border); border-radius: 12px; background: var(--surface-2); font-size: 11.5px; color: var(--text); }
  .chip-t.off { opacity: 0.4; text-decoration: line-through; }
  .chip-t .n { color: var(--text-3); font-size: 10.5px; }
  .ln { display: inline-block; width: 14px; height: 0; border-top: 1px solid var(--text-2); }
  .ln.injects { border-top-width: 2.5px; }
  .ln.implements { border-top-style: dashed; }
  .ln.feign { border-top: 3px solid #06b6d4; }
  .pk { display: grid; gap: 2px; max-height: 180px; overflow: auto; }
  .pk label { display: flex; align-items: center; gap: 6px; font-size: 11.5px; }
  .pk .mono { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }
  .only { border: 0; background: none; padding: 0; font-size: 10.5px; color: var(--accent-text); }
  .list { display: grid; gap: 1px; }
  .list button, .hs { display: flex; align-items: center; gap: 6px; padding: 4px 6px; border: 0; border-radius: var(--r); background: none; text-align: left; font-size: 12px; color: var(--text); width: 100%; }
  .list button:hover, .hs:hover, .mem:hover { background: var(--hover); }
  .list .faint, .hs .faint { margin-left: auto; font-size: 11px; white-space: nowrap; }
  .tg { display: flex; align-items: center; gap: 5px; font-size: 12px; }
  .note { margin: 0; font-size: 11.5px; line-height: 1.5; color: var(--text-2); }
  .good { margin: 0; color: var(--good); font-size: 12px; }
  .card { padding: 7px 9px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); font-size: 12px; }
  .names { display: flex; flex-wrap: wrap; gap: 3px 10px; }
  .names button, .inl { border: 0; background: none; padding: 0; color: var(--accent-text); font: inherit; font-size: 12px; }
  .names button.fe { color: #06b6d4; font-weight: 600; }
  .small { font-size: 11px; }
  .title { display: flex; align-items: center; gap: 6px; font-size: 13.5px; min-width: 0; }
  .title b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .close { margin-left: auto; border: 0; background: none; font-size: 16px; color: var(--text-3); line-height: 1; }
  .feign { color: #06b6d4; font-weight: 600; }
  .nums { display: flex; flex-wrap: wrap; gap: 3px 10px; font-size: 11.5px; color: var(--text-2); }
  .nums b { color: var(--text); font-variant-numeric: tabular-nums; }
  .nums .bad, .nums .bad b { color: var(--bad); }
  .acts { display: flex; flex-wrap: wrap; gap: 4px; }
  .mem { display: flex; align-items: center; gap: 6px; width: 100%; padding: 3px 4px; border: 0; border-radius: var(--r); background: none; text-align: left; font-size: 11.5px; min-width: 0; color: var(--text); }
  .mem.ep { display: block; font-size: 10.5px; color: var(--text-2); overflow-wrap: anywhere; }
  .nm { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }
  .v { max-width: 45%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 10.5px; color: var(--good); }
  .ic { width: 14px; height: 14px; flex-shrink: 0; display: grid; place-items: center; border-radius: 3px; font: 600 9.5px var(--mono); background: var(--surface-3); color: var(--text-2); }
  .ic.k { color: var(--good); } .ic.m { color: var(--accent-text); }
  .cost { flex-shrink: 0; font: 500 10px var(--mono); color: var(--text-3); }
  .cost.hot { color: var(--bad); font-weight: 600; }
  .hint { margin: 2px 0 4px; font-size: 11px; color: var(--text-3); }
</style>
