<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import cytoscape, { type Core, type ElementDefinition, type StylesheetJson } from 'cytoscape';
  import fcose from 'cytoscape-fcose';
  import { app, nodeFor, openFile, saveList, selectNode, toast } from '../lib/store.svelte';
  import { cssVar, SEVERITY_COLOR, STATE_COLOR, STEREOTYPE, stereo, worst } from '../lib/theme';
  import { cycles, dependencies, dependents, layeredPositions, layerViolations, metrics, path } from '../lib/graphAnalysis';
  import Glyph from './Glyph.svelte';
  import GraphPanel from './GraphPanel.svelte';
  import GraphGuide from './GraphGuide.svelte';
  import type { Graph, GraphNode, Severity } from '../lib/types';

  cytoscape.use(fcose);

  let container: HTMLDivElement;
  let cy: Core | null = null;
  let builtKey = '';
  let hover = $state<{ x: number; y: number; node: GraphNode } | null>(null);
  let menu = $state<{ x: number; y: number; id: string } | null>(null);

  const isTest = (n: GraphNode) => n.stereotype === 'test' || n.file.includes('src/test/');
  const m = $derived(app.graph ? metrics(app.graph) : new Map());

  /** The types and links that pass the filters (roles, packages, hidden types, tests, link kinds, "only matches"). */
  const visible = $derived.by(() => {
    const g = app.graph;
    if (!g) return { nodes: [] as GraphNode[], edges: [] as Graph['edges'] };
    const q = app.graphQuery.trim().toLowerCase();
    const roles = new Set(app.hiddenRoles);
    const pkgs = new Set(app.hiddenPkgs);
    const hidden = new Set(app.hiddenNodes);
    const nodes = g.nodes.filter((n) =>
      (app.showTests || !isTest(n)) && !roles.has(n.stereotype) && !pkgs.has(n.pkg) && !hidden.has(n.id) &&
      (app.graphQueryMode !== 'only' || !q || matches(n, q)));
    const ids = new Set(nodes.map((n) => n.id));
    const kinds = new Set(app.hiddenEdges);
    const edges = g.edges.filter((e) => ids.has(e.source) && ids.has(e.target) && !kinds.has(e.kind));
    return { nodes, edges };
  });

  function matches(n: GraphNode, q: string) {
    return n.name.toLowerCase().includes(q) || n.pkg.toLowerCase().includes(q) || n.annotations.some((a) => a.toLowerCase().includes(q));
  }

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

  /** Open issues per type, for sizing by issues. */
  const issuesPerNode = $derived.by(() => {
    const out = new Map<string, number>();
    for (const i of app.issueList?.issues ?? []) {
      if (i.state !== 'open') continue;
      const id = nodeFor(i.path, i.line);
      if (id) out.set(id, (out.get(id) ?? 0) + 1);
    }
    return out;
  });

  function size(id: string, degree: number): number {
    if (app.sizeBy === 'lines') return Math.min(56, 14 + 3.2 * Math.sqrt(m.get(id)?.lines ?? 1));
    if (app.sizeBy === 'issues') return Math.min(56, 16 + 9 * Math.sqrt(issuesPerNode.get(id) ?? 0));
    return Math.min(46, 14 + 7 * Math.sqrt(degree));
  }

  function elements(): ElementDefinition[] {
    const { nodes, edges } = visible;
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
      out.push({
        group: 'nodes',
        data: {
          id: n.id, name: n.name, label: n.name, kind: n.kind, size: size(n.id, degree.get(n.id) ?? 0), degree: degree.get(n.id) ?? 0,
          stereotype: n.stereotype, shape: stereo(n.stereotype).shape, ...(app.groupPackages ? { parent: 'pkg:' + n.pkg } : {}),
        },
      });
    }
    for (const e of edges) out.push({ group: 'edges', data: { id: `${e.source}>${e.target}`, source: e.source, target: e.target, kind: e.kind } });
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
    const glyph = cssVar('--node-glyph');
    const glyphs = Object.entries(STEREOTYPE).map(([k, s]) => ({
      selector: `node[stereotype = "${k}"]`,
      style: { 'background-image': glyphImage(s.glyph, glyph), 'background-width': '62%', 'background-height': '62%', 'background-clip': 'none' },
    }));
    return [
      { selector: 'node', style: {
        label: app.showLabels ? 'data(label)' : '', color: label, 'font-family': 'Inter Variable, sans-serif', 'font-size': 11, 'font-weight': 500,
        'text-valign': 'bottom', 'text-margin-y': 5, 'text-outline-color': bg, 'text-outline-width': 2.5, 'min-zoomed-font-size': 7,
        'transition-property': 'opacity, border-width, border-color, underlay-opacity', 'transition-duration': 180,
      } },
      { selector: 'node[size]', style: {
        width: 'data(size)', height: 'data(size)', shape: 'data(shape)' as any, 'background-color': cssVar('--node-fill'),
        'border-width': 1.5, 'border-color': cssVar('--node-border'), 'underlay-opacity': 0, 'underlay-padding': 5, 'underlay-shape': 'ellipse',
      } },
      ...(glyphs as any),
      { selector: 'node[kind = "interface"], node[kind = "abstract"]', style: { 'border-style': 'dashed' } },
      { selector: 'node[stereotype = "test"]', style: { opacity: 0.7 } },
      { selector: ':parent', style: {
        shape: 'round-rectangle', 'background-color': label, 'background-opacity': 0.03, 'border-width': 1, 'border-style': 'dashed', 'border-color': edge,
        label: 'data(label)', 'text-valign': 'top', 'text-halign': 'center', 'text-margin-y': -4, color: cssVar('--text-3'), 'font-size': 10,
        'font-family': 'JetBrains Mono Variable, monospace', padding: '14px',
      } as any },
      { selector: 'edge', style: {
        width: 1, 'line-color': edge, 'curve-style': 'unbundled-bezier', 'control-point-distances': [16], 'control-point-weights': [0.5],
        'target-arrow-shape': 'triangle', 'target-arrow-color': edge, 'arrow-scale': 0.6, 'transition-property': 'opacity, line-color, width', 'transition-duration': 180,
      } as any },
      { selector: 'edge[kind = "injects"]', style: { width: 1.8 } },
      { selector: 'edge[kind = "extends"], edge[kind = "implements"]', style: { 'target-arrow-shape': 'triangle-backcurve', 'target-arrow-fill': 'hollow', 'arrow-scale': 0.9 } as any },
      { selector: 'edge[kind = "implements"]', style: { 'line-style': 'dashed' } },

      { selector: 'node.changed', style: { 'border-color': STATE_COLOR.changed, 'border-width': 3, 'underlay-color': STATE_COLOR.changed, 'underlay-opacity': 0.18, 'font-weight': 600, color: cssVar('--text') } },
      { selector: 'node.added', style: { 'border-color': STATE_COLOR.added, 'border-width': 3, 'underlay-color': STATE_COLOR.added, 'underlay-opacity': 0.18, 'font-weight': 600, color: cssVar('--text') } },
      { selector: 'node.impacted', style: { 'border-color': STATE_COLOR.impacted, 'border-width': 2, 'border-style': 'dashed' } },
      { selector: 'node.issue', style: { 'underlay-color': 'data(issueColor)', 'underlay-opacity': 0.28, 'underlay-padding': 8, 'border-color': 'data(issueColor)', 'border-width': 3 } },
      { selector: 'node.target', style: { 'border-color': 'data(issueColor)', 'border-width': 4, 'underlay-color': 'data(issueColor)', 'underlay-opacity': 0.45, 'underlay-padding': 12, 'font-weight': 700, color: cssVar('--text') } },
      { selector: 'node.picked', style: { 'border-color': accent, 'border-width': 3, 'underlay-color': accent, 'underlay-opacity': 0.2 } },
      { selector: 'node.match', style: { 'underlay-color': accent, 'underlay-opacity': 0.35, 'font-weight': 700, color: cssVar('--text') } },
      { selector: 'edge.impact', style: { 'line-color': STATE_COLOR.impacted, 'target-arrow-color': STATE_COLOR.impacted, width: 1.8 } },
      { selector: 'edge.hl', style: { 'line-color': accent, 'target-arrow-color': accent, width: 1.8 } },
      // Analysis overlays
      { selector: 'node.cycle', style: { 'border-color': '#e11d48', 'border-width': 3, 'underlay-color': '#e11d48', 'underlay-opacity': 0.18 } },
      { selector: 'edge.cycle', style: { 'line-color': '#e11d48', 'target-arrow-color': '#e11d48', width: 2.4 } },
      { selector: 'edge.violation', style: { 'line-color': '#a855f7', 'target-arrow-color': '#a855f7', width: 2.4, 'line-style': 'dashed' } },
      { selector: 'node.origin', style: { 'border-color': accent, 'border-width': 4, 'underlay-color': accent, 'underlay-opacity': 0.3, 'font-weight': 700, color: cssVar('--text') } },
      { selector: 'node.d1', style: { 'underlay-color': '#f97316', 'underlay-opacity': 0.45, 'border-color': '#f97316', 'border-width': 3 } },
      { selector: 'node.d2', style: { 'underlay-color': '#f97316', 'underlay-opacity': 0.28, 'border-color': '#fb923c', 'border-width': 2 } },
      { selector: 'node.d3', style: { 'underlay-color': '#f97316', 'underlay-opacity': 0.14, 'border-color': '#fdba74', 'border-width': 2 } },
      { selector: 'node.onpath', style: { 'border-color': '#06b6d4', 'border-width': 3, 'underlay-color': '#06b6d4', 'underlay-opacity': 0.28, 'font-weight': 700, color: cssVar('--text') } },
      { selector: 'edge.onpath', style: { 'line-color': '#06b6d4', 'target-arrow-color': '#06b6d4', width: 2.8 } },
      { selector: '.dim', style: { opacity: 0.14 } },
      { selector: ':parent.dim', style: { opacity: 0.45 } },
      { selector: '.fade', style: { opacity: 0.1 } },
    ];
  }

  const fcoseLayout = {
    name: 'fcose', quality: 'proof', nodeDimensionsIncludeLabels: true, packComponents: true, uniformNodeDimensions: false,
    nodeRepulsion: () => 7000,
    idealEdgeLength: (e: cytoscape.EdgeSingular) => 55 + 6 * Math.sqrt(e.source().degree(false) + e.target().degree(false)),
    edgeElasticity: () => 0.35, nestingFactor: 0.4, gravity: 0.22, gravityRange: 3.2, gravityCompound: 1.4, numIter: 4000,
    tile: true, tilingPaddingVertical: 20, tilingPaddingHorizontal: 20, padding: 40,
  };

  function layout(incremental: boolean, extra: Record<string, unknown> = {}) {
    if (!cy || cy.nodes().empty()) return;
    const anim = { animate: true, animationDuration: incremental ? 500 : 700, animationEasing: 'ease-out-cubic', fit: !incremental, padding: 40 };
    let opts: Record<string, unknown>;
    switch (incremental ? 'natural' : app.layoutMode) {
      case 'layers': {
        const pos = layeredPositions(visible.nodes, container.clientWidth);
        opts = { name: 'preset', positions: (n: cytoscape.NodeSingular) => pos.get(n.id()) ?? { x: 0, y: 0 } };
        break;
      }
      case 'hubs':
        opts = { name: 'concentric', concentric: (n: cytoscape.NodeSingular) => n.degree(false), levelWidth: () => 2, minNodeSpacing: 34 };
        break;
      case 'grid':
        opts = { name: 'grid', avoidOverlap: true, condense: true, sort: (a: cytoscape.NodeSingular, b: cytoscape.NodeSingular) => String(a.data('stereotype')).localeCompare(String(b.data('stereotype'))) };
        break;
      default:
        opts = { ...fcoseLayout, randomize: !incremental };
    }
    cy.layout({ ...opts, ...anim, ...extra } as unknown as cytoscape.LayoutOptions).run();
  }

  function sync() {
    if (!cy || !app.graph) return;
    const els = elements();
    const key = [app.groupPackages, app.layoutMode].join('|');
    const rebuild = key !== builtKey || cy.elements().empty();
    builtKey = key;
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
        cy!.add(e.group === 'nodes' && !e.data.isPkg ? { ...e, position: near(e.data.id!) } : e);
      }
    });
    if (structural) layout(app.layoutMode !== 'natural' ? false : true);
  }

  function near(id: string) {
    for (const e of app.graph?.edges ?? []) {
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

  const cycleList = $derived(app.graph ? cycles(visible.nodes.map((n) => n.id), visible.edges.filter((e) => e.kind === 'uses' || e.kind === 'injects')) : []);
  const violationList = $derived(app.graph ? layerViolations({ ...app.graph, nodes: visible.nodes, edges: visible.edges }) : []);

  function highlight() {
    if (!cy) return;
    const a = app.detail?.analysis;
    const picked = app.selectedNode;
    const q = app.graphQuery.trim().toLowerCase() || app.search.trim().toLowerCase();
    const commitScope = app.scope === 'commit' || app.scope === 'working';

    cy.batch(() => {
      cy!.elements().removeClass('changed added impacted issue impact hl picked dim match target cycle violation origin d1 d2 d3 onpath');
      let keep: cytoscape.Collection | null = null;

      // The selected commit, or the uncommitted changes: what changed and what depends on it.
      const changed = new Set<string>();
      if (app.scope === 'working' && app.working) {
        const files = new Set(app.working.changes.map((c) => c.newPath || c.path));
        cy!.nodes().forEach((n) => {
          const node = app.graph?.nodes.find((x) => x.id === n.id());
          if (node && files.has(node.file)) { n.addClass('changed'); changed.add(n.id()); }
        });
      } else if (a) {
        for (const c of a.changed) {
          const n = cy!.getElementById(c.id);
          if (n.empty()) continue;
          n.addClass(c.status === 'added' ? 'added' : 'changed');
          changed.add(c.id);
        }
        const impacted = new Set(a.impacted);
        for (const id of impacted) cy!.getElementById(id).addClass('impacted');
        cy!.edges().forEach((e) => { if (changed.has(e.target().id()) && impacted.has(e.source().id())) e.addClass('impact'); });
      }

      // The issues the hub lists, as rings in the worst severity per type.
      const byNode = new Map<string, { severity: Severity }[]>();
      for (const g of app.hub) for (const i of g) {
        const id = nodeFor(i.path, i.line);
        if (id) byNode.set(id, [...(byNode.get(id) ?? []), i]);
      }
      for (const [id, list] of byNode) {
        const n = cy!.getElementById(id);
        if (n.nonempty()) n.addClass('issue').data('issueColor', SEVERITY_COLOR[worst(list)]);
      }

      if (app.focusChanges && app.overlay === 'none') {
        const seeds = commitScope && changed.size ? cy!.nodes('.changed, .added') : !commitScope && byNode.size ? cy!.nodes('.issue') : null;
        if (seeds) {
          let k = cy!.collection();
          seeds.forEach((n) => { k = k.union(n.closedNeighborhood()); });
          keep = k.union(k.nodes().ancestors());
        }
      }

      // Analysis overlays replace the focus.
      if (app.overlay === 'cycles') {
        let k = cy!.collection();
        for (const c of cycleList) {
          const set = new Set(c);
          for (const id of c) k = k.union(cy!.getElementById(id).addClass('cycle'));
          cy!.edges().forEach((e) => { if (set.has(e.source().id()) && set.has(e.target().id()) && (e.data('kind') === 'uses' || e.data('kind') === 'injects')) { e.addClass('cycle'); k = k.union(e); } });
        }
        keep = k.nonempty() ? k.union(k.nodes().ancestors()) : null;
      } else if (app.overlay === 'layers') {
        let k = cy!.collection();
        for (const v of violationList) {
          const e = cy!.getElementById(`${v.source}>${v.target}`);
          if (e.nonempty()) k = k.union(e.addClass('violation')).union(e.connectedNodes());
        }
        keep = k.nonempty() ? k.union(k.nodes().ancestors()) : null;
      } else if (app.overlay === 'blast' && app.overlayNode && app.graph) {
        const origin = cy!.getElementById(app.overlayNode);
        let k = origin.addClass('origin');
        for (const [id, d] of dependents(app.graph, app.overlayNode)) {
          const n = cy!.getElementById(id);
          if (n.nonempty()) k = k.union(n.addClass(d === 1 ? 'd1' : d === 2 ? 'd2' : 'd3'));
        }
        k = k.union(k.edgesWith(k));
        keep = k.union(k.nodes().ancestors());
      } else if (app.overlay === 'path' && app.pathFrom && app.pathTo && app.graph) {
        const p = path({ ...app.graph, edges: visible.edges }, app.pathFrom, app.pathTo);
        let k = cy!.collection();
        p.forEach((id, i) => {
          k = k.union(cy!.getElementById(id).addClass('onpath'));
          if (i > 0) k = k.union(cy!.getElementById(`${p[i - 1]}>${id}`).addClass('onpath'));
        });
        keep = k.nonempty() ? k.union(k.nodes().ancestors()) : null;
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
          if (app.overlay === 'none') keep = hood.union(hood.nodes().ancestors());
        }
      }
      if (q && app.graphQueryMode === 'highlight') {
        const hits = cy!.nodes().filter((n) => !n.data('isPkg') && matches(app.graph!.nodes.find((x) => x.id === n.id())!, q));
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

  export function fit() {
    cy?.animate({ fit: { eles: cy.elements().not('.dim'), padding: 40 } }, { duration: 400 });
  }

  function exportPng() {
    if (!cy) return;
    const url = cy.png({ full: true, scale: 2, bg: cssVar('--canvas') });
    const a = document.createElement('a');
    a.href = url;
    a.download = `${app.status?.repo ?? 'codist'}-graph.png`;
    a.click();
    toast('Graph exported as PNG', 'good', 1500);
  }

  // Right-click menu actions.
  function hide(id: string) {
    app.hiddenNodes = [...new Set([...app.hiddenNodes, id])];
    saveList('hiddenNodes', app.hiddenNodes);
  }
  function isolate(id: string, which: 'neighbours' | 'dependents' | 'dependencies') {
    if (!app.graph) return;
    const keep = new Set([id]);
    if (which === 'neighbours') for (const e of app.graph.edges) { if (e.source === id) keep.add(e.target); if (e.target === id) keep.add(e.source); }
    else for (const k of (which === 'dependents' ? dependents : dependencies)(app.graph, id).keys()) keep.add(k);
    app.hiddenNodes = app.graph.nodes.filter((n) => !keep.has(n.id)).map((n) => n.id);
    saveList('hiddenNodes', app.hiddenNodes);
    requestAnimationFrame(() => layout(false));
  }
  function menuAction(action: string) {
    const id = menu?.id;
    menu = null;
    if (!id || !app.graph) return;
    const node = app.graph.nodes.find((n) => n.id === id);
    switch (action) {
      case 'open': if (node) { selectNode(id); app.showPanel = true; } break;
      case 'code': if (node) { app.showPanel = true; void openFile(node.file, null, node.startLine); } break;
      case 'hide': hide(id); break;
      case 'neighbours': case 'dependents': case 'dependencies': isolate(id, action); break;
      case 'blast': app.overlay = 'blast'; app.overlayNode = id; app.showGraphPanel = true; break;
      case 'from': app.pathFrom = id; app.overlay = 'path'; app.showGraphPanel = true; break;
      case 'to': app.pathTo = id; app.overlay = 'path'; app.showGraphPanel = true; break;
      case 'copy': void navigator.clipboard?.writeText(id); toast('Copied ' + id, 'good', 1200); break;
    }
  }

  onMount(() => {
    cy = cytoscape({ container, minZoom: 0.15, maxZoom: 3, boxSelectionEnabled: false, style: styles() });
    cy.on('tap', 'node', (e) => {
      if (e.target.data('isPkg')) return;
      selectNode(e.target.id());
      app.showPanel = true;
    });
    cy.on('tap', (e) => { menu = null; if (e.target === cy) app.selectedNode = null; });
    cy.on('cxttap', 'node', (e) => {
      if (e.target.data('isPkg')) return;
      const p = e.renderedPosition;
      menu = { x: p.x, y: p.y, id: e.target.id() };
      hover = null;
    });
    // Hovering a type lights up what it touches and shows its numbers.
    cy.on('mouseover', 'node', (e) => {
      const n = e.target;
      if (n.data('isPkg') || menu) return;
      const hood = n.closedNeighborhood();
      cy!.elements().not(hood.union(hood.nodes().ancestors())).addClass('fade');
      const node = app.graph?.nodes.find((x) => x.id === n.id());
      const p = n.renderedPosition();
      if (node) hover = { x: p.x, y: p.y - n.renderedHeight() / 2, node };
      container.style.cursor = 'pointer';
    });
    cy.on('mouseout', 'node', () => {
      cy!.elements().removeClass('fade');
      hover = null;
      container.style.cursor = '';
    });
    cy.on('pan zoom grab', () => { hover = null; menu = null; });
    cy.on('dragfree', 'node', (e) => {
      const n = e.target;
      if (n.data('isPkg') || app.layoutMode !== 'natural') return;
      layout(true, { numIter: 600, fixedNodeConstraint: [{ nodeId: n.id(), position: { ...n.position() } }], animationDuration: 400 });
    });
    if (app.graph) { sync(); highlight(); }

    const onResize = () => cy?.resize();
    const onCenter = (e: Event) => center((e as CustomEvent<string>).detail);
    const onRelayout = () => layout(false);
    const onFit = () => fit();
    const onPng = () => exportPng();
    window.addEventListener('codist:resize', onResize);
    window.addEventListener('resize', onResize);
    window.addEventListener('codist:center', onCenter);
    window.addEventListener('codist:relayout', onRelayout);
    window.addEventListener('codist:fit', onFit);
    window.addEventListener('codist:png', onPng);
    return () => {
      for (const [n, f] of [['codist:resize', onResize], ['resize', onResize], ['codist:center', onCenter], ['codist:relayout', onRelayout], ['codist:fit', onFit], ['codist:png', onPng]] as const)
        window.removeEventListener(n, f as EventListener);
      cy?.destroy();
    };
  });

  $effect(() => {
    void app.graph; void app.groupPackages; void app.layoutMode; void visible; void app.sizeBy; void issuesPerNode;
    untrack(() => { sync(); highlight(); });
  });

  $effect(() => {
    void app.detail; void app.hub; void app.pickedIssue; void app.scope; void app.working; void app.selectedNode;
    void app.focusChanges; void app.search; void app.graphQuery; void app.graphQueryMode;
    void app.overlay; void app.overlayNode; void app.pathFrom; void app.pathTo; void cycleList; void violationList;
    untrack(highlight);
  });

  $effect(() => {
    void app.theme; void app.showLabels;
    untrack(() => requestAnimationFrame(() => cy?.style(styles())));
  });

  const legend = $derived.by(() => {
    const present = new Set(visible.nodes.map((n) => n.stereotype));
    return Object.entries(STEREOTYPE).filter(([k]) => present.has(k)).map(([k, s]) => ({ ...s, key: k }));
  });
  const hidden = $derived((app.graph?.nodes.length ?? 0) - visible.nodes.length);
  const hm = $derived(hover ? m.get(hover.node.id) : null);
</script>

<section class="graph">
  <div class="canvas" bind:this={container}></div>

  <div class="toolbar">
    <div class="seg">
      <button class:on={app.focusChanges} onclick={() => (app.focusChanges = !app.focusChanges)} title="Dim what is not relevant: the commit's changes in commit scope, otherwise the types with listed issues">Focus</button>
      <button class:on={app.groupPackages} onclick={() => (app.groupPackages = !app.groupPackages)} title="Draw packages as boxes">Packages</button>
      <button class:on={app.showTests} onclick={() => (app.showTests = !app.showTests)} title="Show test classes">Tests</button>
    </div>
    <div class="seg icons">
      <button title="Re-run layout" aria-label="Re-run layout" onclick={() => layout(false)}>
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M16 10a6 6 0 1 1-1.8-4.3M16 3v3.5h-3.5"/></svg>
      </button>
      <button title="Fit to view" aria-label="Fit to view" onclick={fit}>
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M3 7V3h4M13 3h4v4M17 13v4h-4M7 17H3v-4"/></svg>
      </button>
      <button class:on={app.showLegend} title="{app.showLegend ? 'Hide' : 'Show'} legend" aria-label="Toggle legend" onclick={() => (app.showLegend = !app.showLegend)}>
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="5" cy="6" r="1.5"/><circle cx="5" cy="14" r="1.5"/><path d="M9 6h8M9 14h8"/></svg>
      </button>
      <button title="What do the shapes, colours and lines mean?" aria-label="Graph guide" onclick={() => (app.showGuide = true)}>
        <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="10" cy="10" r="7.5"/><path d="M7.8 7.6a2.3 2.3 0 1 1 3.2 2.1c-.6.3-1 .8-1 1.5v.4M10 14.6v.2"/></svg>
      </button>
    </div>
    <button class="controls" class:on={app.showGraphPanel} onclick={() => (app.showGraphPanel = !app.showGraphPanel)} title="Find, filter, layouts and analysis">
      <svg width="14" height="14" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M3 5h9M15 5h2M3 10h3M9 10h8M3 15h7M13 15h4"/><circle cx="13.5" cy="5" r="1.6"/><circle cx="7.5" cy="10" r="1.6"/><circle cx="11.5" cy="15" r="1.6"/></svg>
      Controls
      {#if hidden > 0}<span class="hid" title="{hidden} type{hidden === 1 ? '' : 's'} hidden by filters">{hidden} hidden</span>{/if}
      {#if app.overlay !== 'none'}<span class="ov">{app.overlay}</span>{/if}
    </button>
  </div>

  {#if hover && hm}
    <div class="tip" style:left="{hover.x}px" style:top="{hover.y}px">
      <div class="th"><Glyph stereotype={hover.node.stereotype} /><b>{hover.node.name}</b><span class="faint">{stereo(hover.node.stereotype).label}{hover.node.kind !== 'class' ? ' · ' + hover.node.kind : ''}</span></div>
      <div class="mono faint pkg">{hover.node.pkg}</div>
      <div class="nums">
        <span title="Types that depend on this one"><b>{hm.fanIn}</b> in</span>
        <span title="Types this one depends on"><b>{hm.fanOut}</b> out</span>
        <span title="Instability = out / (in + out). 0 = stable, many rely on it; 1 = depends on others, nothing relies on it"><b>{hm.instability.toFixed(2)}</b> instability</span>
        <span><b>{hm.lines}</b> lines</span>
        {#if issuesPerNode.get(hover.node.id)}<span class="bad"><b>{issuesPerNode.get(hover.node.id)}</b> open issues</span>{/if}
      </div>
      <div class="faint hint">Right-click for more</div>
    </div>
  {/if}

  {#if menu}
    {@const n = app.graph?.nodes.find((x) => x.id === menu?.id)}
    <div class="menu" role="menu" style:left="{menu.x}px" style:top="{menu.y}px">
      <div class="mh"><b>{n?.name}</b></div>
      <button role="menuitem" onclick={() => menuAction('open')}>Open details</button>
      <button role="menuitem" onclick={() => menuAction('code')}>Open code</button>
      <hr />
      <button role="menuitem" onclick={() => menuAction('blast')}>Blast radius: what depends on it</button>
      <button role="menuitem" onclick={() => menuAction('neighbours')}>Show only it and its neighbours</button>
      <button role="menuitem" onclick={() => menuAction('dependencies')}>Show only what it depends on</button>
      <button role="menuitem" onclick={() => menuAction('dependents')}>Show only what depends on it</button>
      <button role="menuitem" onclick={() => menuAction('from')}>Path from here…</button>
      <button role="menuitem" onclick={() => menuAction('to')}>Path to here</button>
      <hr />
      <button role="menuitem" onclick={() => menuAction('hide')}>Hide this type</button>
      <button role="menuitem" onclick={() => menuAction('copy')}>Copy class name</button>
    </div>
  {/if}

  {#if app.showLegend}
    <div class="legend" class:shifted={app.showGraphPanel}>
      {#each legend as s}<span><Glyph stereotype={s.key} />{s.label}</span>{/each}
      <span class="sep"></span>
      <span><i class="ring" style:border-color={STATE_COLOR.changed}></i>Changed</span>
      <span><i class="ring" style:border-color={STATE_COLOR.added}></i>Added</span>
      <span><i class="ring dashed" style:border-color={STATE_COLOR.impacted}></i>Impacted</span>
      <span><i class="ring" style:border-color={STATE_COLOR.issue}></i>Has issues</span>
      <button class="more" onclick={() => (app.showGuide = true)}>What does it all mean?</button>
    </div>
  {/if}

  {#if app.showGraphPanel}<GraphPanel cycles={cycleList} violations={violationList} metrics={m} {visible} />{/if}
  {#if app.showGuide}<GraphGuide />{/if}

  {#if app.graph && !app.graph.nodes.length}
    <div class="empty overlay">No Java types found in this repository.</div>
  {:else if app.graph && !visible.nodes.length}
    <div class="empty overlay">All types are hidden by the filters. <button class="btn secondary sm" onclick={() => { app.hiddenRoles = []; app.hiddenPkgs = []; app.hiddenNodes = []; app.graphQuery = ''; saveList('hiddenRoles', []); saveList('hiddenPkgs', []); saveList('hiddenNodes', []); }}>Show everything</button></div>
  {/if}
</section>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') { menu = null; app.showGuide = false; } }} />

<style>
  .graph { position: relative; min-width: 0; min-height: 360px; overflow: hidden; background-color: var(--canvas); background-image: radial-gradient(var(--canvas-dot) 1px, transparent 1px); background-size: 18px 18px; }
  .canvas { position: absolute; inset: 0; }
  .toolbar { position: absolute; top: 10px; left: 10px; right: 10px; display: flex; gap: 6px; z-index: 3; pointer-events: none; }
  .toolbar > * { pointer-events: auto; }
  .seg { display: flex; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg button { height: 24px; padding: 0 10px; border: 0; border-radius: var(--r); background: none; font-size: 12px; color: var(--text-3); transition: background 0.15s, color 0.15s; }
  .seg.icons button { width: 26px; padding: 0; display: grid; place-items: center; }
  .seg button:hover { color: var(--text); }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }
  .controls { margin-left: auto; display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 10px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); color: var(--text-2); font-size: 12px; font-weight: 500; }
  .controls:hover, .controls.on { color: var(--text); border-color: var(--border-strong); background: var(--surface-3); }
  .hid { font-size: 10.5px; padding: 0 6px; border-radius: 9px; background: color-mix(in srgb, var(--warn) 16%, transparent); color: var(--warn); }
  .ov { font-size: 10.5px; padding: 0 6px; border-radius: 9px; background: var(--accent-soft); color: var(--accent-text); text-transform: capitalize; }

  .tip { position: absolute; z-index: 4; transform: translate(-50%, calc(-100% - 10px)); display: grid; gap: 3px; padding: 8px 10px; border: 1px solid var(--border-strong); border-radius: var(--r-lg); background: var(--surface); box-shadow: var(--shadow); font-size: 12px; white-space: nowrap; pointer-events: none; }
  .th { display: flex; align-items: center; gap: 6px; }
  .pkg { font-size: 10.5px; }
  .nums { display: flex; gap: 10px; font-size: 11.5px; color: var(--text-2); }
  .nums b { color: var(--text); font-variant-numeric: tabular-nums; }
  .nums .bad, .nums .bad b { color: var(--bad); }
  .hint { font-size: 10.5px; }

  .menu { position: absolute; z-index: 6; min-width: 230px; padding: 4px; border: 1px solid var(--border-strong); border-radius: var(--r-lg); background: var(--surface); box-shadow: var(--shadow); display: grid; }
  .menu .mh { padding: 6px 10px 4px; font-size: 12px; }
  .menu button { text-align: left; padding: 6px 10px; border: 0; border-radius: var(--r); background: none; font-size: 12.5px; color: var(--text); }
  .menu button:hover { background: var(--hover); }
  .menu hr { border: 0; border-top: 1px solid var(--border); margin: 4px 0; }

  .legend { position: absolute; left: 10px; bottom: 10px; max-width: calc(100% - 20px); display: flex; flex-wrap: wrap; gap: 4px 12px; padding: 6px 10px; border: 1px solid var(--border); border-radius: var(--r-lg); background: color-mix(in srgb, var(--surface) 90%, transparent); font-size: 11.5px; color: var(--text-2); z-index: 2; }
  .legend.shifted { max-width: calc(100% - 340px); }
  .legend span { display: inline-flex; align-items: center; gap: 5px; }
  .legend i.ring { background: transparent; border: 2px solid; width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
  .legend i.dashed { border-style: dashed; }
  .legend .sep { width: 1px; height: 12px; background: var(--border-strong); }
  .legend .more { border: 0; background: none; padding: 0; color: var(--accent-text); font-size: 11.5px; }
  .legend .more:hover { text-decoration: underline; }
  .overlay { position: absolute; inset: 0; display: flex; gap: 10px; align-items: center; justify-content: center; }
  @media (max-width: 1000px) { .graph { height: 60vh; } }
</style>
