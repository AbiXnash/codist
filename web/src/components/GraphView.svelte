<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import cytoscape, { type Core, type ElementDefinition, type StylesheetJson } from 'cytoscape';
  import fcose from 'cytoscape-fcose';
  import { app, nodeFor, selectNode } from '../lib/store.svelte';
  import { cssVar, SEVERITY_COLOR, STATE_COLOR, STEREOTYPE, stereo, worst } from '../lib/theme';
  import Glyph from './Glyph.svelte';
  import type { Graph, Severity } from '../lib/types';

  cytoscape.use(fcose);

  let container: HTMLDivElement;
  let cy: Core | null = null;
  let built = { group: false, tests: false };
  let hover = $state<{ x: number; y: number; name: string; pkg: string; stereotype: string } | null>(null);

  const isTest = (n: Graph['nodes'][number]) => n.stereotype === 'test' || n.file.includes('src/test/');

  function commonPrefix(pkgs: string[]): string {
    if (!pkgs.length) return '';
    const parts = pkgs.map((p) => p.split('.'));
    const out: string[] = [];
    for (let i = 0; ; i++) {
      const seg = parts[0][i];
      if (seg === undefined || parts.some((p) => p[i] !== seg)) break;
      out.push(seg);
    }
    return out.join('.');
  }

  function elements(g: Graph): ElementDefinition[] {
    const nodes = g.nodes.filter((n) => app.showTests || !isTest(n));
    const ids = new Set(nodes.map((n) => n.id));
    const edges = g.edges.filter((e) => ids.has(e.source) && ids.has(e.target));
    const degree = new Map<string, number>();
    for (const e of edges) {
      degree.set(e.source, (degree.get(e.source) ?? 0) + 1);
      degree.set(e.target, (degree.get(e.target) ?? 0) + 1);
    }
    const pkgs = [...new Set(nodes.map((n) => n.pkg))];
    const prefix = commonPrefix(pkgs);
    const out: ElementDefinition[] = [];
    if (app.groupPackages) {
      for (const pkg of pkgs) {
        const label = pkg === prefix ? pkg.split('.').pop() || '(default)' : pkg.slice(prefix ? prefix.length + 1 : 0);
        out.push({ group: 'nodes', data: { id: 'pkg:' + pkg, label, isPkg: true } });
      }
    }
    for (const n of nodes) {
      // Size follows how connected a type is, so hubs stand out the way they do in the codebase.
      const size = Math.min(44, 14 + 7 * Math.sqrt(degree.get(n.id) ?? 0));
      out.push({
        group: 'nodes',
        data: {
          id: n.id, name: n.name, label: n.name, kind: n.kind, size, stereotype: n.stereotype, shape: stereo(n.stereotype).shape,
          ...(app.groupPackages ? { parent: 'pkg:' + n.pkg } : {}),
        },
      });
    }
    for (const e of edges) {
      out.push({ group: 'edges', data: { id: `${e.source}>${e.target}`, source: e.source, target: e.target, kind: e.kind } });
    }
    return out;
  }

  // A letter drawn inside each node, so roles read without relying on colour.
  function glyphImage(letter: string, color: string): string {
    const svg = `<svg xmlns='http://www.w3.org/2000/svg' width='48' height='48'><text x='24' y='31' font-family='Inter, Helvetica, Arial, sans-serif' font-size='20' font-weight='600' text-anchor='middle' fill='${color}'>${letter}</text></svg>`;
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(svg);
  }

  function styles(): StylesheetJson {
    const bg = cssVar('--canvas');
    const edge = cssVar('--graph-edge');
    const label = cssVar('--graph-label');
    const accent = cssVar('--accent');
    const fill = cssVar('--node-fill');
    const border = cssVar('--node-border');
    const glyph = cssVar('--node-glyph');
    const glyphs = Object.entries(STEREOTYPE).map(([k, s]) => ({
      selector: `node[stereotype = "${k}"]`,
      style: { 'background-image': glyphImage(s.glyph, glyph), 'background-width': '62%', 'background-height': '62%', 'background-clip': 'none' },
    }));
    return [
      { selector: 'node', style: {
        label: 'data(label)', color: label, 'font-family': 'Inter Variable, sans-serif', 'font-size': 11, 'font-weight': 500,
        'text-valign': 'bottom', 'text-margin-y': 5, 'text-outline-color': bg, 'text-outline-width': 2.5,
        'min-zoomed-font-size': 7,
        'transition-property': 'opacity, border-width, border-color, underlay-opacity', 'transition-duration': 180,
      } },
      { selector: 'node[size]', style: {
        width: 'data(size)', height: 'data(size)', shape: 'data(shape)' as any, 'background-color': fill,
        'border-width': 1.5, 'border-color': border,
        'underlay-opacity': 0, 'underlay-padding': 5, 'underlay-shape': 'ellipse',
      } },
      ...(glyphs as any),
      { selector: 'node[kind = "interface"], node[kind = "abstract"]', style: { 'border-style': 'dashed' } },
      { selector: 'node[stereotype = "test"]', style: { opacity: 0.7 } },
      { selector: ':parent', style: {
        shape: 'round-rectangle', 'background-color': label, 'background-opacity': 0.03,
        'border-width': 1, 'border-style': 'dashed', 'border-color': edge,
        label: 'data(label)', 'text-valign': 'top', 'text-halign': 'center', 'text-margin-y': -4,
        color: cssVar('--text-3'), 'font-size': 10, 'font-family': 'JetBrains Mono Variable, monospace', padding: '14px',
      } as any },
      { selector: 'edge', style: {
        width: 1, 'line-color': edge, 'curve-style': 'unbundled-bezier', 'control-point-distances': [16], 'control-point-weights': [0.5],
        'target-arrow-shape': 'triangle', 'target-arrow-color': edge, 'arrow-scale': 0.6,
        'transition-property': 'opacity, line-color, width', 'transition-duration': 180,
      } as any },
      { selector: 'edge[kind = "injects"]', style: { width: 1.4 } },
      { selector: 'edge[kind = "extends"], edge[kind = "implements"]', style: { 'target-arrow-shape': 'triangle-backcurve', 'target-arrow-fill': 'hollow', 'arrow-scale': 0.9 } as any },
      { selector: 'edge[kind = "implements"]', style: { 'line-style': 'dashed' } },

      { selector: 'node.changed', style: { 'border-color': STATE_COLOR.changed, 'border-width': 3, 'underlay-color': STATE_COLOR.changed, 'underlay-opacity': 0.18, 'font-weight': 600, color: cssVar('--text') } },
      { selector: 'node.added', style: { 'border-color': STATE_COLOR.added, 'border-width': 3, 'underlay-color': STATE_COLOR.added, 'underlay-opacity': 0.18, 'font-weight': 600, color: cssVar('--text') } },
      { selector: 'node.impacted', style: { 'border-color': STATE_COLOR.impacted, 'border-width': 2, 'border-style': 'dashed' } },
      { selector: 'node.issue', style: { 'underlay-color': 'data(issueColor)', 'underlay-opacity': 0.28, 'underlay-padding': 8, 'border-color': 'data(issueColor)', 'border-width': 3 } },
      { selector: 'node.target', style: { 'border-color': 'data(issueColor)', 'border-width': 4, 'underlay-color': 'data(issueColor)', 'underlay-opacity': 0.45, 'underlay-padding': 12, 'font-weight': 700, color: cssVar('--text') } },
      { selector: 'node.picked', style: { 'border-color': accent, 'border-width': 3, 'underlay-color': accent, 'underlay-opacity': 0.2 } },
      { selector: 'node.match', style: { 'underlay-color': accent, 'underlay-opacity': 0.35 } },
      { selector: 'edge.impact', style: { 'line-color': STATE_COLOR.impacted, 'target-arrow-color': STATE_COLOR.impacted, width: 1.8 } },
      { selector: 'edge.hl', style: { 'line-color': accent, 'target-arrow-color': accent, width: 1.6 } },
      { selector: '.dim', style: { opacity: 0.14 } },
      { selector: ':parent.dim', style: { opacity: 0.45 } },
      { selector: '.fade', style: { opacity: 0.1 } },
    ];
  }

  const baseLayout = {
    name: 'fcose',
    quality: 'proof',
    nodeDimensionsIncludeLabels: true,
    packComponents: true,
    uniformNodeDimensions: false,
    nodeRepulsion: () => 7000,
    idealEdgeLength: (e: cytoscape.EdgeSingular) => 55 + 6 * Math.sqrt(e.source().degree(false) + e.target().degree(false)),
    edgeElasticity: () => 0.35,
    nestingFactor: 0.4,
    gravity: 0.22,
    gravityRange: 3.2,
    gravityCompound: 1.4,
    numIter: 4000,
    tile: true,
    tilingPaddingVertical: 20,
    tilingPaddingHorizontal: 20,
    padding: 40,
  };

  function layout(incremental: boolean, extra: Record<string, unknown> = {}) {
    if (!cy || cy.nodes().empty()) return;
    cy.layout({
      ...baseLayout,
      randomize: !incremental,
      animate: true,
      animationDuration: incremental ? 500 : 700,
      animationEasing: 'ease-out-cubic',
      fit: !incremental,
      ...extra,
    } as unknown as cytoscape.LayoutOptions).run();
  }

  function sync(g: Graph) {
    if (!cy) return;
    const els = elements(g);
    const rebuild = built.group !== app.groupPackages || built.tests !== app.showTests || cy.elements().empty();
    built = { group: app.groupPackages, tests: app.showTests };
    if (rebuild) {
      cy.elements().remove();
      cy.add(els);
      layout(false);
      return;
    }
    const wanted = new Map(els.map((e) => [e.data.id!, e]));
    let structural = false;
    cy.batch(() => {
      const gone = cy!.elements().filter((el) => !wanted.has(el.id()));
      if (gone.nonempty()) { structural = true; gone.remove(); }
      for (const e of els) {
        const existing = cy!.getElementById(e.data.id!);
        if (existing.nonempty()) {
          const { id, parent, source, target, ...rest } = e.data;
          existing.data(rest);
          continue;
        }
        structural = true;
        cy!.add(e.group === 'nodes' && !e.data.isPkg ? { ...e, position: near(g, e.data.id!) } : e);
      }
    });
    if (structural) layout(true);
  }

  function near(g: Graph, id: string) {
    for (const e of g.edges) {
      const other = e.source === id ? e.target : e.target === id ? e.source : null;
      const n = other && cy!.getElementById(other);
      if (n && n.nonempty()) {
        const p = n.position();
        return { x: p.x + (Math.random() - 0.5) * 80, y: p.y + (Math.random() - 0.5) * 80 };
      }
    }
    const ext = cy!.extent();
    return { x: (ext.x1 + ext.x2) / 2, y: (ext.y1 + ext.y2) / 2 };
  }

  function highlight() {
    if (!cy) return;
    const a = app.detail?.analysis;
    const picked = app.selectedNode;
    const q = app.search.trim().toLowerCase();
    const commitScope = app.scope === 'commit';

    cy.batch(() => {
      cy!.elements().removeClass('changed added impacted issue impact hl picked dim match target');
      let keep: cytoscape.Collection | null = null;

      // The selected commit: what it changed and what depends on that.
      const changed = new Set<string>();
      if (a) {
        for (const c of a.changed) {
          const n = cy!.getElementById(c.id);
          if (n.empty()) continue;
          n.addClass(c.status === 'added' ? 'added' : 'changed');
          changed.add(c.id);
        }
        const impacted = new Set(a.impacted);
        for (const id of impacted) cy!.getElementById(id).addClass('impacted');
        cy!.edges().forEach((e) => {
          if (changed.has(e.target().id()) && impacted.has(e.source().id())) e.addClass('impact');
        });
      }

      // The issues the hub lists, as rings in the worst severity per type.
      const byNode = new Map<string, { severity: Severity }[]>();
      for (const g of app.hub) {
        for (const i of g) {
          const id = i.line ? nodeFor(i.path, i.line) : nodeFor(i.path, 0);
          if (id) byNode.set(id, [...(byNode.get(id) ?? []), i]);
        }
      }
      for (const [id, list] of byNode) {
        const n = cy!.getElementById(id);
        if (n.nonempty()) n.addClass('issue').data('issueColor', SEVERITY_COLOR[worst(list)]);
      }

      if (app.focusChanges) {
        if (commitScope && changed.size) {
          let k = cy!.collection();
          cy!.nodes('.changed, .added').forEach((n) => { k = k.union(n.closedNeighborhood()); });
          keep = k.union(k.nodes().ancestors());
        } else if (!commitScope && byNode.size) {
          let k = cy!.collection();
          cy!.nodes('.issue').forEach((n) => { k = k.union(n.closedNeighborhood()); });
          keep = k.union(k.nodes().ancestors());
        }
      }

      // The issue open in the hub: its places stand out.
      const targets = (app.pickedIssue ?? []).map((i) => nodeFor(i.path, i.line)).filter((x): x is string => !!x);
      if (targets.length) {
        let k = cy!.collection();
        for (const id of targets) {
          const n = cy!.getElementById(id);
          if (n.empty()) continue;
          n.addClass('target');
          k = k.union(n.closedNeighborhood());
          n.connectedEdges().addClass('hl');
        }
        keep = k.union(k.nodes().ancestors());
      }

      if (picked) {
        const n = cy!.getElementById(picked);
        if (n.nonempty()) {
          n.addClass('picked');
          const hood = n.closedNeighborhood();
          hood.edges().addClass('hl');
          keep = hood.union(hood.nodes().ancestors());
        }
      }
      if (q) {
        const hits = cy!.nodes().filter((n) => !n.data('isPkg') && String(n.data('name')).toLowerCase().includes(q));
        hits.addClass('match');
        keep = hits.union(hits.ancestors());
      }
      if (keep) cy!.elements().not(keep).addClass('dim');
    });
  }

  function center(id: string) {
    const n = cy?.getElementById(id);
    if (n && n.nonempty()) cy!.animate({ center: { eles: n }, zoom: Math.max(cy!.zoom(), 1.2) }, { duration: 400, easing: 'ease-out-cubic' });
  }

  onMount(() => {
    cy = cytoscape({ container, minZoom: 0.15, maxZoom: 3, boxSelectionEnabled: false, style: styles() });

    cy.on('tap', 'node', (e) => {
      if (e.target.data('isPkg')) return;
      selectNode(e.target.id());
      app.showPanel = true;
    });
    cy.on('tap', (e) => { if (e.target === cy) app.selectedNode = null; });

    // Hovering a type lights up what it touches.
    cy.on('mouseover', 'node', (e) => {
      const n = e.target;
      if (n.data('isPkg')) return;
      const hood = n.closedNeighborhood();
      cy!.elements().not(hood.union(hood.nodes().ancestors())).addClass('fade');
      const node = app.graph?.nodes.find((x) => x.id === n.id());
      const p = n.renderedPosition();
      if (node) hover = { x: p.x, y: p.y - n.renderedHeight() / 2, name: node.name, pkg: node.pkg, stereotype: node.stereotype };
      container.style.cursor = 'pointer';
    });
    cy.on('mouseout', 'node', () => {
      cy!.elements().removeClass('fade');
      hover = null;
      container.style.cursor = '';
    });
    cy.on('pan zoom grab', () => (hover = null));

    // After a drag, let the neighbourhood settle around the node where it was dropped.
    cy.on('dragfree', 'node', (e) => {
      const n = e.target;
      if (n.data('isPkg')) return;
      layout(true, { numIter: 600, fixedNodeConstraint: [{ nodeId: n.id(), position: { ...n.position() } }], animationDuration: 400 });
    });

    if (app.graph) { sync(app.graph); highlight(); }

    const onResize = () => cy?.resize();
    const onCenter = (e: Event) => center((e as CustomEvent<string>).detail);
    window.addEventListener('codist:resize', onResize);
    window.addEventListener('resize', onResize);
    window.addEventListener('codist:center', onCenter);
    return () => {
      window.removeEventListener('codist:resize', onResize);
      window.removeEventListener('resize', onResize);
      window.removeEventListener('codist:center', onCenter);
      cy?.destroy();
    };
  });

  $effect(() => {
    const g = app.graph;
    void app.groupPackages;
    void app.showTests;
    if (g) untrack(() => { sync(g); highlight(); });
  });

  $effect(() => {
    void app.detail;
    void app.hub;
    void app.pickedIssue;
    void app.scope;
    void app.selectedNode;
    void app.focusChanges;
    void app.search;
    untrack(highlight);
  });

  $effect(() => {
    void app.theme;
    untrack(() => requestAnimationFrame(() => cy?.style(styles())));
  });

  const legend = $derived.by(() => {
    const present = new Set((app.graph?.nodes ?? []).filter((n) => app.showTests || !isTest(n)).map((n) => n.stereotype));
    return Object.entries(STEREOTYPE).filter(([k]) => present.has(k)).map(([k, s]) => ({ ...s, key: k }));
  });
</script>

<section class="graph">
  <div class="canvas" bind:this={container}></div>

  <div class="toolbar">
    <div class="seg">
      <button class:on={app.focusChanges} onclick={() => (app.focusChanges = !app.focusChanges)} title="Dim what is not relevant: the commit's changes in commit scope, otherwise types with listed issues">Focus</button>
      <button class:on={app.groupPackages} onclick={() => (app.groupPackages = !app.groupPackages)} title="Group types by package">Packages</button>
      <button class:on={app.showTests} onclick={() => (app.showTests = !app.showTests)} title="Show test classes">Tests</button>
    </div>
    <div class="seg icons">
      <button title="Re-run layout" aria-label="Re-run layout" onclick={() => layout(false)}>
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M16 10a6 6 0 1 1-1.8-4.3M16 3v3.5h-3.5"/></svg>
      </button>
      <button title="Fit to view" aria-label="Fit to view" onclick={() => cy?.animate({ fit: { eles: cy.elements(), padding: 40 } }, { duration: 400 })}>
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M3 7V3h4M13 3h4v4M17 13v4h-4M7 17H3v-4"/></svg>
      </button>
      <button class:on={app.showLegend} title="{app.showLegend ? 'Hide' : 'Show'} legend" aria-label="Toggle legend" onclick={() => (app.showLegend = !app.showLegend)}>
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="5" cy="6" r="1.5"/><circle cx="5" cy="14" r="1.5"/><path d="M9 6h8M9 14h8"/></svg>
      </button>
    </div>
  </div>

  {#if hover}
    <div class="tip" style:left="{hover.x}px" style:top="{hover.y}px">
      <Glyph stereotype={hover.stereotype} /><b>{hover.name}</b>
      <span class="mono faint">{hover.pkg}</span>
    </div>
  {/if}

  {#if app.showLegend}
    <div class="legend">
      {#each legend as s}<span><Glyph stereotype={s.key} />{s.label}</span>{/each}
      <span class="sep"></span>
      <span><i class="ring" style:border-color={STATE_COLOR.changed}></i>Changed</span>
      <span><i class="ring" style:border-color={STATE_COLOR.added}></i>Added</span>
      <span><i class="ring dashed" style:border-color={STATE_COLOR.impacted}></i>Impacted</span>
      <span><i class="ring" style:border-color={STATE_COLOR.issue}></i>Has issues (as listed)</span>
      <span class="faint">dashed = interface / abstract</span>
    </div>
  {/if}

  {#if app.graph && !app.graph.nodes.length}
    <div class="empty overlay">No Java types found in this repository.</div>
  {/if}
</section>

<style>
  .graph { position: relative; min-width: 0; min-height: 360px; background-color: var(--canvas); background-image: radial-gradient(var(--canvas-dot) 1px, transparent 1px); background-size: 18px 18px; }
  .canvas { position: absolute; inset: 0; }

  .toolbar { position: absolute; top: 10px; left: 10px; display: flex; gap: 6px; z-index: 2; }
  .seg { display: flex; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg button {
    height: 24px;
    padding: 0 10px;
    border: 0;
    border-radius: var(--r);
    background: none;
    font-size: 12px;
    color: var(--text-3);
    transition: background 0.15s, color 0.15s;
  }
  .seg.icons button { width: 26px; padding: 0; display: grid; place-items: center; }
  .seg button:hover { color: var(--text); }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }

  .tip {
    position: absolute;
    z-index: 3;
    transform: translate(-50%, calc(-100% - 8px));
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 8px;
    border: 1px solid var(--border-strong);
    border-radius: var(--r);
    background: var(--surface);
    box-shadow: var(--shadow);
    font-size: 12px;
    white-space: nowrap;
    pointer-events: none;
  }
  .tip .mono { font-size: 11px; }

  .legend {
    position: absolute;
    left: 10px;
    bottom: 10px;
    max-width: calc(100% - 20px);
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    padding: 6px 10px;
    border: 1px solid var(--border);
    border-radius: var(--r-lg);
    background: color-mix(in srgb, var(--surface) 90%, transparent);
    font-size: 11.5px;
    color: var(--text-2);
    z-index: 2;
  }
  .legend span { display: inline-flex; align-items: center; gap: 5px; }
  .legend i { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
  .legend i.ring { background: transparent; border: 2px solid; width: 10px; height: 10px; }
  .legend i.dashed { border-style: dashed; }
  .legend .sep { width: 1px; height: 12px; background: var(--border-strong); }
  .overlay { position: absolute; inset: 0; }

  @media (max-width: 1000px) { .graph { height: 60vh; } }
</style>
