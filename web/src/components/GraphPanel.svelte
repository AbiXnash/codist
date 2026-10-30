<script lang="ts">
  import { app, saveList, savePref, saveStr, selectNode } from '../lib/store.svelte';
  import { STEREOTYPE, stereo } from '../lib/theme';
  import { dependents, path, type Metrics, type Violation } from '../lib/graphAnalysis';
  import Glyph from './Glyph.svelte';
  import type { Graph, GraphNode } from '../lib/types';

  let { cycles, violations, metrics, visible }: {
    cycles: string[][]; violations: Violation[]; metrics: Map<string, Metrics>; visible: { nodes: GraphNode[]; edges: Graph['edges'] };
  } = $props();

  let open = $state<Record<string, boolean>>({ find: true, show: true, layout: true, analysis: true, hotspots: false });
  const all = $derived(app.graph?.nodes ?? []);
  const byId = $derived(new Map(all.map((n) => [n.id, n])));
  const name = (id: string) => byId.get(id)?.name ?? id.slice(id.lastIndexOf('.') + 1);

  const roles = $derived.by(() => {
    const c = new Map<string, number>();
    for (const n of all) c.set(n.stereotype, (c.get(n.stereotype) ?? 0) + 1);
    return Object.keys(STEREOTYPE).filter((k) => c.has(k)).map((k) => ({ key: k, n: c.get(k)! }));
  });
  const pkgs = $derived.by(() => {
    const c = new Map<string, number>();
    for (const n of all) c.set(n.pkg, (c.get(n.pkg) ?? 0) + 1);
    return [...c].sort((a, b) => a[0].localeCompare(b[0]));
  });
  const edgeKinds = $derived([...new Set((app.graph?.edges ?? []).map((e) => e.kind))]);
  const q = $derived(app.graphQuery.trim().toLowerCase());
  const hits = $derived(q ? all.filter((n) => n.name.toLowerCase().includes(q) || n.pkg.toLowerCase().includes(q) || n.annotations.some((a) => a.toLowerCase().includes(q))) : []);

  function toggle(list: 'hiddenRoles' | 'hiddenPkgs' | 'hiddenEdges' | 'hiddenNodes', v: string) {
    const cur = new Set(app[list]);
    if (cur.has(v)) cur.delete(v);
    else cur.add(v);
    app[list] = [...cur];
    saveList(list, app[list]);
  }
  function only(list: 'hiddenRoles' | 'hiddenPkgs', keep: string, values: string[]) {
    app[list] = values.filter((v) => v !== keep);
    saveList(list, app[list]);
  }
  function showAll() {
    for (const l of ['hiddenRoles', 'hiddenPkgs', 'hiddenEdges', 'hiddenNodes'] as const) { app[l] = []; saveList(l, []); }
    app.graphQuery = '';
    requestAnimationFrame(() => window.dispatchEvent(new Event('codist:relayout')));
  }
  function setLayout(v: typeof app.layoutMode) {
    app.layoutMode = v;
    saveStr('layoutMode', v);
  }
  function setSize(v: typeof app.sizeBy) {
    app.sizeBy = v;
    saveStr('sizeBy', v);
  }
  function focusNode(id: string) {
    selectNode(id);
    app.showPanel = true;
    window.dispatchEvent(new CustomEvent('codist:center', { detail: id }));
  }
  function find(e: KeyboardEvent) {
    if (e.key === 'Enter' && hits[0]) focusNode(hits[0].id);
    if (e.key === 'Escape') app.graphQuery = '';
  }

  const blast = $derived(app.overlay === 'blast' && app.overlayNode && app.graph ? [...dependents(app.graph, app.overlayNode)].sort((a, b) => a[1] - b[1]) : []);
  const route = $derived(app.overlay === 'path' && app.pathFrom && app.pathTo && app.graph ? path({ ...app.graph, edges: visible.edges }, app.pathFrom, app.pathTo) : []);
  const reverseRoute = $derived(app.overlay === 'path' && app.pathFrom && app.pathTo && app.graph && !route.length ? path({ ...app.graph, edges: visible.edges }, app.pathTo, app.pathFrom) : []);

  const top = (f: (m: Metrics, id: string) => number, n = 5) =>
    visible.nodes.map((x) => ({ id: x.id, v: f(metrics.get(x.id)!, x.id) })).filter((x) => x.v > 0).sort((a, b) => b.v - a.v).slice(0, n);
  const mostUsed = $derived(top((m) => m.fanIn));
  const fragile = $derived(top((m) => (m.fanIn >= 1 && m.fanOut >= 2 ? m.instability * m.fanIn : 0)));
  const largest = $derived(top((m) => m.lines));
  const sorted = $derived([...visible.nodes].sort((a, b) => a.name.localeCompare(b.name)));
</script>

<aside class="panel" aria-label="Graph controls">
  <header>
    <b>Graph controls</b>
    <span class="faint">{visible.nodes.length} of {all.length} types · {visible.edges.length} links</span>
    <button class="x" aria-label="Close" onclick={() => { app.showGraphPanel = false; savePref('graphPanel', false); }}>×</button>
  </header>

  <section>
    <button class="sh" onclick={() => (open.find = !open.find)}>Find</button>
    {#if open.find}
      <input class="in" bind:value={app.graphQuery} onkeydown={find} placeholder="Class, package or @Annotation…" spellcheck="false" />
      <div class="row">
        <div class="seg">
          <button class:on={app.graphQueryMode === 'highlight'} onclick={() => (app.graphQueryMode = 'highlight')}>Highlight</button>
          <button class:on={app.graphQueryMode === 'only'} onclick={() => (app.graphQueryMode = 'only')}>Only matches</button>
        </div>
        {#if q}<span class="faint small">{hits.length} match{hits.length === 1 ? '' : 'es'} · ↵ jumps</span>{/if}
      </div>
      {#if q && hits.length}
        <div class="list">{#each hits.slice(0, 8) as h}<button onclick={() => focusNode(h.id)}><Glyph stereotype={h.stereotype} />{h.name}<span class="faint mono">{h.pkg.split('.').pop()}</span></button>{/each}</div>
      {/if}
    {/if}
  </section>

  <section>
    <button class="sh" onclick={() => (open.show = !open.show)}>Show</button>
    {#if open.show}
      <div class="label">Roles <span class="faint">click to hide · double-click for only this</span></div>
      <div class="chips">
        {#each roles as r}
          <button class="chip-t" class:off={app.hiddenRoles.includes(r.key)} onclick={() => toggle('hiddenRoles', r.key)} ondblclick={() => only('hiddenRoles', r.key, roles.map((x) => x.key))}>
            <Glyph stereotype={r.key} />{stereo(r.key).label}<span class="n">{r.n}</span>
          </button>
        {/each}
      </div>
      <div class="label">Links</div>
      <div class="chips">
        {#each edgeKinds as k}
          <button class="chip-t" class:off={app.hiddenEdges.includes(k)} onclick={() => toggle('hiddenEdges', k)} title={k === 'injects' ? 'A field, constructor parameter or @Autowired dependency' : k === 'uses' ? 'Refers to the type: calls, parameters, return types, locals' : k}>
            <i class="ln {k}"></i>{k}
          </button>
        {/each}
        <button class="chip-t" class:off={!app.showTests} onclick={() => (app.showTests = !app.showTests)}>tests</button>
      </div>
      <div class="label">Packages</div>
      <div class="pk">
        {#each pkgs as [p, n]}
          <label><input type="checkbox" checked={!app.hiddenPkgs.includes(p)} onchange={() => toggle('hiddenPkgs', p)} /><span class="mono">{p}</span><span class="faint">{n}</span></label>
        {/each}
      </div>
      {#if app.hiddenNodes.length}
        <div class="label">Hidden types <button class="lnk" onclick={() => { app.hiddenNodes = []; saveList('hiddenNodes', []); }}>show all {app.hiddenNodes.length}</button></div>
        <div class="list">{#each app.hiddenNodes.slice(0, 12) as id}<button onclick={() => toggle('hiddenNodes', id)}>{name(id)}<span class="faint">show</span></button>{/each}</div>
      {/if}
      {#if app.hiddenRoles.length || app.hiddenPkgs.length || app.hiddenNodes.length || app.hiddenEdges.length || q}
        <button class="btn secondary sm" onclick={showAll}>Reset filters</button>
      {/if}
    {/if}
  </section>

  <section>
    <button class="sh" onclick={() => (open.layout = !open.layout)}>Layout</button>
    {#if open.layout}
      <div class="seg wide">
        <button class:on={app.layoutMode === 'natural'} onclick={() => setLayout('natural')} title="Force-directed: related types pull together">Natural</button>
        <button class:on={app.layoutMode === 'layers'} onclick={() => setLayout('layers')} title="Controllers on top, then services, repositories, entities and DTOs">Layers</button>
        <button class:on={app.layoutMode === 'hubs'} onclick={() => setLayout('hubs')} title="The most connected types in the middle">Hubs</button>
        <button class:on={app.layoutMode === 'grid'} onclick={() => setLayout('grid')} title="A tidy grid grouped by role">Grid</button>
      </div>
      <div class="label">Size by</div>
      <div class="seg wide">
        <button class:on={app.sizeBy === 'links'} onclick={() => setSize('links')}>Links</button>
        <button class:on={app.sizeBy === 'lines'} onclick={() => setSize('lines')}>Code size</button>
        <button class:on={app.sizeBy === 'issues'} onclick={() => setSize('issues')}>Open issues</button>
      </div>
      <label class="tg"><input type="checkbox" bind:checked={app.showLabels} onchange={() => savePref('labels', app.showLabels)} /> Labels</label>
      <label class="tg"><input type="checkbox" bind:checked={app.groupPackages} /> Package boxes</label>
      <button class="btn secondary sm" onclick={() => window.dispatchEvent(new Event('codist:png'))}>Export PNG</button>
    {/if}
  </section>

  <section>
    <button class="sh" onclick={() => (open.analysis = !open.analysis)}>Analysis</button>
    {#if open.analysis}
      <div class="seg wide">
        <button class:on={app.overlay === 'none'} onclick={() => (app.overlay = 'none')}>Off</button>
        <button class:on={app.overlay === 'cycles'} onclick={() => (app.overlay = 'cycles')} title="Types that depend on each other in a loop">Cycles {cycles.length}</button>
        <button class:on={app.overlay === 'layers'} onclick={() => (app.overlay = 'layers')} title="Dependencies that break the controller → service → repository → entity layering">Layers {violations.length}</button>
        <button class:on={app.overlay === 'blast'} onclick={() => (app.overlay = 'blast')} title="What depends on a type, directly or indirectly">Blast</button>
        <button class:on={app.overlay === 'path'} onclick={() => (app.overlay = 'path')} title="How one type reaches another">Path</button>
      </div>

      {#if app.overlay === 'cycles'}
        <p class="note">A cycle is a loop of dependencies: none of its types can change, be tested or be reused alone. Break it with an interface or by moving the shared part out.</p>
        {#each cycles as c, i}
          <div class="card"><b>Cycle {i + 1}</b> · {c.length} types<div class="names">{#each c as id}<button onclick={() => focusNode(id)}>{name(id)}</button>{/each}</div></div>
        {:else}<p class="good">No dependency cycles.</p>{/each}
      {:else if app.overlay === 'layers'}
        <p class="note">Spring layering: controllers → services → repositories → entities. Arrows going up, or controllers reaching a repository directly, bypass business rules and transactions.</p>
        {#each violations as v}
          <div class="card"><button onclick={() => focusNode(v.source)}>{name(v.source)}</button> → <button onclick={() => focusNode(v.target)}>{name(v.target)}</button><div class="faint small">{v.why}</div></div>
        {:else}<p class="good">No layer violations.</p>{/each}
      {:else if app.overlay === 'blast'}
        <select class="in" bind:value={app.overlayNode}><option value={null}>Pick a type…</option>{#each sorted as n}<option value={n.id}>{n.name}</option>{/each}</select>
        {#if app.overlayNode}
          <p class="note">If <b>{name(app.overlayNode)}</b> changes, these may break (darker = closer). Re-test them.</p>
          {#each [1, 2, 3] as d}
            {@const at = blast.filter(([, x]) => (d === 3 ? x >= 3 : x === d))}
            {#if at.length}<div class="label">{d === 1 ? 'Directly' : d === 2 ? 'Through one other' : 'Further away'} · {at.length}</div>
              <div class="names">{#each at as [id]}<button onclick={() => focusNode(id)}>{name(id)}</button>{/each}</div>{/if}
          {/each}
          {#if !blast.length}<p class="good">Nothing depends on it.</p>{/if}
        {:else}<p class="faint small">Or right-click a type → Blast radius.</p>{/if}
      {:else if app.overlay === 'path'}
        <div class="row2">
          <select class="in" bind:value={app.pathFrom}><option value={null}>From…</option>{#each sorted as n}<option value={n.id}>{n.name}</option>{/each}</select>
          <select class="in" bind:value={app.pathTo}><option value={null}>To…</option>{#each sorted as n}<option value={n.id}>{n.name}</option>{/each}</select>
        </div>
        {#if app.pathFrom && app.pathTo}
          {#if route.length}<p class="note">{route.length - 1} step{route.length === 2 ? '' : 's'}: {#each route as id, i}{i ? ' → ' : ''}<button class="inl" onclick={() => focusNode(id)}>{name(id)}</button>{/each}</p>
          {:else if reverseRoute.length}<p class="note">No path that way, but {name(app.pathTo)} reaches {name(app.pathFrom)} in {reverseRoute.length - 1} steps.</p>
          {:else}<p class="note">These types are not connected (with the current filters).</p>{/if}
        {/if}
      {/if}
    {/if}
  </section>

  <section>
    <button class="sh" onclick={() => (open.hotspots = !open.hotspots)}>Hotspots</button>
    {#if open.hotspots}
      <div class="label">Most depended on <span class="faint">changes here ripple</span></div>
      {#each mostUsed as h}<button class="hs" onclick={() => focusNode(h.id)}>{name(h.id)}<span class="faint">{h.v} in</span></button>{/each}
      <div class="label">Fragile <span class="faint">depends on much, and others depend on it</span></div>
      {#each fragile as h}<button class="hs" onclick={() => focusNode(h.id)}>{name(h.id)}<span class="faint">in {metrics.get(h.id)?.fanIn} · out {metrics.get(h.id)?.fanOut}</span></button>{:else}<p class="faint small">None.</p>{/each}
      <div class="label">Largest</div>
      {#each largest as h}<button class="hs" onclick={() => focusNode(h.id)}>{name(h.id)}<span class="faint">{h.v} lines</span></button>{/each}
    {/if}
  </section>
</aside>

<style>
  .panel { position: absolute; top: 48px; right: 10px; bottom: 10px; width: 310px; z-index: 5; display: flex; flex-direction: column; overflow: auto; border: 1px solid var(--border-strong); border-radius: var(--r-lg); background: color-mix(in srgb, var(--surface) 97%, transparent); box-shadow: var(--shadow); font-size: 12.5px; }
  header { position: sticky; top: 0; z-index: 1; display: flex; align-items: baseline; gap: 8px; padding: 10px 12px; background: var(--surface); border-bottom: 1px solid var(--border); }
  header .faint { font-size: 11px; }
  .x { margin-left: auto; border: 0; background: none; font-size: 16px; color: var(--text-3); }
  section { padding: 8px 12px 10px; border-bottom: 1px solid var(--border); display: grid; gap: 7px; }
  .sh { justify-self: start; border: 0; background: none; padding: 0; font-size: 11px; font-weight: 600; color: var(--text-3); text-transform: uppercase; letter-spacing: 0.05em; }
  .sh:hover { color: var(--text); }
  .label { font-size: 11px; color: var(--text-2); font-weight: 500; }
  .label .faint { font-weight: 400; }
  .in { width: 100%; height: 28px; padding: 0 9px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); color: var(--text); font: inherit; font-size: 12px; }
  .in:focus { outline: none; border-color: var(--accent); }
  .row { display: flex; align-items: center; gap: 8px; }
  .row2 { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; }
  .seg { display: flex; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg.wide button { flex: 1; }
  .seg button { height: 24px; padding: 0 8px; border: 0; border-radius: var(--r); background: none; font-size: 11.5px; color: var(--text-3); white-space: nowrap; }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }
  .chips { display: flex; flex-wrap: wrap; gap: 4px; }
  .chip-t { display: inline-flex; align-items: center; gap: 5px; height: 24px; padding: 0 8px; border: 1px solid var(--border); border-radius: 12px; background: var(--surface-2); font-size: 11.5px; color: var(--text); }
  .chip-t.off { opacity: 0.4; text-decoration: line-through; }
  .chip-t .n { color: var(--text-3); font-size: 10.5px; }
  .ln { display: inline-block; width: 14px; height: 0; border-top: 1px solid var(--text-2); }
  .ln.injects { border-top-width: 2.5px; }
  .ln.implements { border-top-style: dashed; }
  .pk { display: grid; gap: 2px; max-height: 150px; overflow: auto; }
  .pk label { display: flex; align-items: center; gap: 6px; font-size: 11.5px; }
  .pk .mono { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 11px; }
  .list { display: grid; gap: 1px; }
  .list button, .hs { display: flex; align-items: center; gap: 6px; padding: 4px 6px; border: 0; border-radius: var(--r); background: none; text-align: left; font-size: 12px; color: var(--text); }
  .list button:hover, .hs:hover { background: var(--hover); }
  .list .faint, .hs .faint { margin-left: auto; font-size: 11px; }
  .lnk { border: 0; background: none; padding: 0; color: var(--accent-text); font-size: 11px; }
  .tg { display: flex; align-items: center; gap: 6px; font-size: 12px; }
  .note { margin: 0; font-size: 11.5px; line-height: 1.5; color: var(--text-2); }
  .good { margin: 0; color: var(--good); font-size: 12px; }
  .card { padding: 7px 9px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); font-size: 12px; }
  .card button, .names button, .inl { border: 0; background: none; padding: 0; color: var(--accent-text); font: inherit; }
  .names { display: flex; flex-wrap: wrap; gap: 4px 10px; margin-top: 3px; }
  .small { font-size: 11px; }
</style>
