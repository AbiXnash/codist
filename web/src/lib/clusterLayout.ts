// Lays out groups (services, packages) as separate boxes: each group is arranged on its own, then the
// boxes are placed so distance means something. Related boxes sit close, unrelated ones further away,
// and no two overlap. Deterministic: the same graph always gets the same picture.

export interface Pt { x: number; y: number }
export interface Item { id: string; w: number; h: number }
export interface Link { a: string; b: string; len: number }
export interface Cluster { id: string; items: Item[]; links: Link[] }
export interface ClusterResult { positions: Map<string, Pt>; boxes: Map<string, Pt & { w: number; h: number }> }

const radius = (i: { w: number; h: number }) => Math.max(i.w, i.h) / 2;

/** A small force simulation: springs pull linked items to their length, everything else repels. */
export function arrange(items: Item[], links: Link[], gap: number): Map<string, Pt> {
  const pos = new Map<string, Pt>();
  if (!items.length) return pos;
  const avg = items.reduce((s, i) => s + radius(i), 0) / items.length + gap / 2;
  // A spiral start, so there is no randomness and nothing starts on top of anything else.
  items.forEach((it, i) => {
    const r = avg * 1.2 * Math.sqrt(i + 1), t = i * 2.399963;
    pos.set(it.id, { x: r * Math.cos(t), y: r * Math.sin(t) });
  });
  const known = links.filter((l) => l.a !== l.b && pos.has(l.a) && pos.has(l.b));
  const spacing = avg * 1.15; // repulsion only matters between neighbours, so the picture stays compact
  const rounds = items.length > 60 ? 160 : 320;
  for (let round = 0; round < rounds; round++) {
    const heat = avg * 0.6 * (1 - round / rounds) + 0.5;
    const move = new Map(items.map((i) => [i.id, { x: 0, y: 0 }]));
    for (let i = 0; i < items.length; i++) {
      const a = pos.get(items[i].id)!;
      for (let j = i + 1; j < items.length; j++) {
        const b = pos.get(items[j].id)!;
        const dx = a.x - b.x, dy = a.y - b.y;
        const d = Math.max(1, Math.hypot(dx, dy));
        if (d > spacing * 3) continue;
        const f = (0.3 * spacing * spacing) / d / d;
        const ma = move.get(items[i].id)!, mb = move.get(items[j].id)!;
        ma.x += dx * f; ma.y += dy * f;
        mb.x -= dx * f; mb.y -= dy * f;
      }
    }
    for (const l of known) {
      const a = pos.get(l.a)!, b = pos.get(l.b)!;
      const dx = b.x - a.x, dy = b.y - a.y;
      const d = Math.max(1, Math.hypot(dx, dy));
      const f = ((d - l.len) / d) * 0.35;
      const ma = move.get(l.a)!, mb = move.get(l.b)!;
      ma.x += dx * f; ma.y += dy * f;
      mb.x -= dx * f; mb.y -= dy * f;
    }
    for (const it of items) {
      const p = pos.get(it.id)!, m = move.get(it.id)!;
      // A gentle pull to the middle keeps unrelated items from drifting off.
      m.x -= p.x * 0.1; m.y -= p.y * 0.1;
      const d = Math.hypot(m.x, m.y);
      const step = d > heat ? heat / d : 1;
      p.x += m.x * step; p.y += m.y * step;
    }
  }
  separate(items, pos, gap);
  return pos;
}

/** Pushes overlapping rectangles apart along whichever axis needs the smaller move. */
export function separate(items: Item[], pos: Map<string, Pt>, gap: number): void {
  for (let round = 0; round < 200; round++) {
    let moved = false;
    for (let i = 0; i < items.length; i++) {
      for (let j = i + 1; j < items.length; j++) {
        const a = items[i], b = items[j];
        const pa = pos.get(a.id)!, pb = pos.get(b.id)!;
        const ox = (a.w + b.w) / 2 + gap - Math.abs(pa.x - pb.x);
        const oy = (a.h + b.h) / 2 + gap - Math.abs(pa.y - pb.y);
        if (ox <= 0 || oy <= 0) continue;
        moved = true;
        if (ox < oy) {
          const s = (pa.x < pb.x ? -1 : pa.x > pb.x ? 1 : i < j ? -1 : 1) * (ox / 2 + 0.5);
          pa.x += s; pb.x -= s;
        } else {
          const s = (pa.y < pb.y ? -1 : pa.y > pb.y ? 1 : i < j ? -1 : 1) * (oy / 2 + 0.5);
          pa.y += s; pb.y -= s;
        }
      }
    }
    if (!moved) return;
  }
}

/** Arranges each cluster, then the clusters. `between` lengths are the wanted gap between box edges. */
export function clusterLayout(clusters: Cluster[], between: Link[], opts: { itemGap?: number; boxGap?: number; pad?: number } = {}): ClusterResult {
  const { itemGap = 12, boxGap = 36, pad = 18 } = opts;
  const offsets = new Map<string, Map<string, Pt>>();
  const boxes: Item[] = [];
  for (const c of clusters) {
    const inner = arrange(c.items, c.links, itemGap);
    let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
    for (const it of c.items) {
      const p = inner.get(it.id)!;
      x0 = Math.min(x0, p.x - it.w / 2); x1 = Math.max(x1, p.x + it.w / 2);
      y0 = Math.min(y0, p.y - it.h / 2); y1 = Math.max(y1, p.y + it.h / 2);
    }
    const cx = (x0 + x1) / 2, cy = (y0 + y1) / 2;
    for (const p of inner.values()) { p.x -= cx; p.y -= cy; }
    offsets.set(c.id, inner);
    boxes.push({ id: c.id, w: x1 - x0 + pad * 2, h: y1 - y0 + pad * 2 });
  }
  const size = new Map(boxes.map((b) => [b.id, b]));
  const links = between.filter((l) => size.has(l.a) && size.has(l.b) && l.a !== l.b)
    .map((l) => ({ ...l, len: l.len + radius(size.get(l.a)!) + radius(size.get(l.b)!) }));
  const centres = arrange(boxes, links, boxGap);
  const positions = new Map<string, Pt>();
  const placed = new Map<string, Pt & { w: number; h: number }>();
  for (const b of boxes) {
    const c = centres.get(b.id)!;
    placed.set(b.id, { x: c.x, y: c.y, w: b.w, h: b.h });
    for (const [id, p] of offsets.get(b.id)!) positions.set(id, { x: c.x + p.x, y: c.y + p.y });
  }
  return { positions, boxes: placed };
}
