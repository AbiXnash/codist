import type { Graph, GraphNode } from './types';

/**
 * Structural analysis of the type graph: metrics, dependency cycles, layer violations, paths and
 * blast radius. Edges point from the type that depends to the type it depends on.
 */

export interface Metrics {
  fanIn: number; // types that depend on this one
  fanOut: number; // types this one depends on
  instability: number; // fanOut / (fanIn + fanOut): 0 = stable (many rely on it), 1 = unstable
  lines: number;
  members: number;
}

export function metrics(g: Graph): Map<string, Metrics> {
  const out = new Map<string, Metrics>();
  for (const n of g.nodes) out.set(n.id, { fanIn: 0, fanOut: 0, instability: 0, lines: Math.max(1, n.endLine - n.startLine + 1), members: n.members.length });
  for (const e of g.edges) {
    if (e.source === e.target) continue;
    const s = out.get(e.source);
    const t = out.get(e.target);
    if (s) s.fanOut++;
    if (t) t.fanIn++;
  }
  for (const m of out.values()) m.instability = m.fanIn + m.fanOut ? m.fanOut / (m.fanIn + m.fanOut) : 0;
  return out;
}

/** Strongly connected components with more than one type: each is a dependency cycle. */
export function cycles(nodes: string[], edges: { source: string; target: string }[]): string[][] {
  const adj = new Map<string, string[]>();
  for (const n of nodes) adj.set(n, []);
  for (const e of edges) if (adj.has(e.source) && adj.has(e.target) && e.source !== e.target) adj.get(e.source)!.push(e.target);
  let index = 0;
  const idx = new Map<string, number>();
  const low = new Map<string, number>();
  const stack: string[] = [];
  const on = new Set<string>();
  const out: string[][] = [];
  const visit = (v: string) => {
    idx.set(v, index);
    low.set(v, index++);
    stack.push(v);
    on.add(v);
    for (const w of adj.get(v)!) {
      if (!idx.has(w)) {
        visit(w);
        low.set(v, Math.min(low.get(v)!, low.get(w)!));
      } else if (on.has(w)) low.set(v, Math.min(low.get(v)!, idx.get(w)!));
    }
    if (low.get(v) === idx.get(v)) {
      const comp: string[] = [];
      let w: string;
      do {
        w = stack.pop()!;
        on.delete(w);
        comp.push(w);
      } while (w !== v);
      if (comp.length > 1) out.push(comp);
    }
  };
  for (const n of nodes) if (!idx.has(n)) visit(n);
  return out.sort((a, b) => b.length - a.length);
}

/** Architectural layer of a role: higher layers may depend on lower ones, not the other way round. */
export const LAYER: Record<string, number> = { controller: 3, advice: 3, service: 2, component: 2, repository: 1, entity: 0, dto: 0 };
export const LAYER_NAME = ['Model (entities, DTOs)', 'Data access (repositories)', 'Business (services)', 'Web (controllers)'];

export interface Violation {
  source: string;
  target: string;
  why: string;
}

/** Dependencies that break the usual Spring layering: upward ones, and controllers that skip the services. */
export function layerViolations(g: Graph): Violation[] {
  const role = new Map(g.nodes.map((n) => [n.id, n.stereotype]));
  const out: Violation[] = [];
  for (const e of g.edges) {
    const s = role.get(e.source) ?? 'other';
    const t = role.get(e.target) ?? 'other';
    const ls = LAYER[s];
    const lt = LAYER[t];
    if (ls === undefined || lt === undefined || e.source === e.target) continue;
    if (lt > ls) out.push({ source: e.source, target: e.target, why: `${s} depends on ${t}: a lower layer reaching up` });
    else if (s === 'controller' && t === 'repository') out.push({ source: e.source, target: e.target, why: 'controller uses a repository directly, skipping the service layer (no transaction or business rules)' });
  }
  return out;
}

/** Shortest dependency path from a to b (following edges), or [] when there is none. */
export function path(g: Graph, a: string, b: string, undirected = false): string[] {
  const adj = new Map<string, string[]>();
  const add = (x: string, y: string) => adj.set(x, [...(adj.get(x) ?? []), y]);
  for (const e of g.edges) {
    add(e.source, e.target);
    if (undirected) add(e.target, e.source);
  }
  const prev = new Map<string, string>([[a, '']]);
  const queue = [a];
  while (queue.length) {
    const v = queue.shift()!;
    if (v === b) break;
    for (const w of adj.get(v) ?? []) {
      if (!prev.has(w)) {
        prev.set(w, v);
        queue.push(w);
      }
    }
  }
  if (!prev.has(b)) return [];
  const out = [b];
  while (out[0] !== a) out.unshift(prev.get(out[0])!);
  return out;
}

/** Everything that depends on id, directly or through others (what may break when it changes), with distance. */
export function dependents(g: Graph, id: string): Map<string, number> {
  const rev = new Map<string, string[]>();
  for (const e of g.edges) rev.set(e.target, [...(rev.get(e.target) ?? []), e.source]);
  const dist = new Map<string, number>([[id, 0]]);
  const queue = [id];
  while (queue.length) {
    const v = queue.shift()!;
    for (const w of rev.get(v) ?? []) {
      if (!dist.has(w)) {
        dist.set(w, dist.get(v)! + 1);
        queue.push(w);
      }
    }
  }
  dist.delete(id);
  return dist;
}

/** Everything id depends on, directly or transitively. */
export function dependencies(g: Graph, id: string): Map<string, number> {
  const adj = new Map<string, string[]>();
  for (const e of g.edges) adj.set(e.source, [...(adj.get(e.source) ?? []), e.target]);
  const dist = new Map<string, number>([[id, 0]]);
  const queue = [id];
  while (queue.length) {
    const v = queue.shift()!;
    for (const w of adj.get(v) ?? []) {
      if (!dist.has(w)) {
        dist.set(w, dist.get(v)! + 1);
        queue.push(w);
      }
    }
  }
  dist.delete(id);
  return dist;
}

/** Positions for the layered layout: one row per layer, controllers on top, grouped by package. */
export function layeredPositions(nodes: GraphNode[], width: number): Map<string, { x: number; y: number }> {
  const rows = new Map<number, GraphNode[]>();
  for (const n of nodes) {
    const l = LAYER[n.stereotype] ?? (n.stereotype === 'test' ? 4 : -1); // config, exceptions, other: their own row
    rows.set(l, [...(rows.get(l) ?? []), n]);
  }
  const order = [4, 3, 2, 1, 0, -1].filter((l) => rows.has(l));
  const out = new Map<string, { x: number; y: number }>();
  order.forEach((l, r) => {
    const row = rows.get(l)!.sort((a, b) => a.pkg.localeCompare(b.pkg) || a.name.localeCompare(b.name));
    const gap = Math.min(170, Math.max(120, width / Math.max(row.length, 1) / 1.6));
    row.forEach((n, i) => out.set(n.id, { x: (i - (row.length - 1) / 2) * gap, y: r * 150 }));
  });
  return out;
}
