// Dragging a node pulls what it is connected to, as if the links were elastic: direct neighbours follow
// most, their neighbours a little, and busy nodes resist. A spring moves them, so they ease into place
// and settle after the node is let go.
import type { Core, NodeCollection, NodeSingular } from 'cytoscape';

const FOLLOW = [0.6, 0.22]; // how much of the drag reaches one and two links away
const MOST = 80; // beyond this a busy node's neighbours stay put, so a drag stays smooth
const STEP = 1000 / 120; // the spring runs at a fixed rate, so it feels the same on any display
const PULL = 0.05, KEEP = 0.86;

interface Follower { node: NodeSingular; weight: number; x: number; y: number; px: number; py: number; vx: number; vy: number }

const links = (n: NodeSingular): NodeCollection =>
  n.connectedEdges().filter((e) => e.data('kind') !== 'member' && !e.hasClass('shared')).connectedNodes().not(n)
    .filter((o) => !o.data('member') && (o as NodeSingular).parent().same(n.parent()));

/** Each neighbour's share of the drag. A busy node has more holding it in place, so it moves less. */
function shares(root: NodeSingular): Map<NodeSingular, number> {
  const out = new Map<NodeSingular, number>();
  const seen = new Set([root.id()]);
  let ring = [root];
  for (const base of FOLLOW) {
    const next: NodeSingular[] = [];
    for (const n of ring) {
      links(n).forEach((o) => {
        if (seen.has(o.id()) || seen.size > MOST) return;
        seen.add(o.id());
        next.push(o);
        out.set(o, base / (1 + Math.log2(Math.max(1, o.degree(false))) / 4));
      });
    }
    ring = next;
  }
  return out;
}

/** A box being dragged pulls the boxes linked to it, by moving what is inside them. */
function boxShares(box: NodeSingular): Map<NodeSingular, number> {
  const out = new Map<NodeSingular, number>();
  const inside = box.descendants();
  const others = inside.connectedEdges().not('.shared').connectedNodes().not(inside).parents().not(box);
  others.forEach((other) => {
    (other as NodeSingular).descendants().filter((n) => !n.isParent()).forEach((n) => { out.set(n as NodeSingular, FOLLOW[0] / 1.5); });
  });
  return out;
}

/** After a drag, boxes that now overlap move out of the way, and any they then reach do the same. */
export function pushBoxesApart(cy: Core, moved: NodeSingular): void {
  const GAP = 40;
  const boxes = cy.nodes(':parent').filter((b) => !b.parent().nonempty());
  const shift = new Map<string, { x: number; y: number }>();
  const placed = new Set([moved.id()]);
  const rect = (b: NodeSingular) => { const r = b.boundingBox({}), d = shift.get(b.id()) ?? { x: 0, y: 0 }; return { x1: r.x1 + d.x, x2: r.x2 + d.x, y1: r.y1 + d.y, y2: r.y2 + d.y }; };
  for (let round = 0, queue = [moved]; round < 40 && queue.length; round++) {
    const fixed = queue.shift()!, f = rect(fixed);
    boxes.forEach((o) => {
      const other = o as NodeSingular;
      if (placed.has(other.id())) return;
      const r = rect(other), ox = Math.min(f.x2, r.x2) - Math.max(f.x1, r.x1) + GAP, oy = Math.min(f.y2, r.y2) - Math.max(f.y1, r.y1) + GAP;
      if (ox <= 0 || oy <= 0) return;
      const d = shift.get(other.id()) ?? { x: 0, y: 0 };
      if (ox < oy) d.x += (r.x1 + r.x2 < f.x1 + f.x2 ? -1 : 1) * ox; else d.y += (r.y1 + r.y2 < f.y1 + f.y2 ? -1 : 1) * oy;
      shift.set(other.id(), d);
      placed.add(other.id());
      queue.push(other);
    });
  }
  const instant = matchMedia('(prefers-reduced-motion: reduce)').matches;
  shift.forEach((d, id) => {
    (cy.getElementById(id) as NodeSingular).descendants().filter((n) => !n.isParent()).forEach((n) => {
      const p = (n as NodeSingular).position(), to = { x: p.x + d.x, y: p.y + d.y };
      if (instant) (n as NodeSingular).position(to); else n.animate({ position:to }, { duration:280, easing:'ease-out-cubic' });
    });
  });
}

export function followOnDrag(cy: Core): void {
  const still = matchMedia('(prefers-reduced-motion: reduce)');
  let held: NodeSingular | null = null;
  let from = { x: 0, y: 0 }, delta = { x: 0, y: 0 };
  let moving: Follower[] = [];
  let frame = 0, last = 0, spare = 0;

  const tick = (now: number) => {
    spare = Math.min(spare + now - last, 50);
    last = now;
    if (held) {
      const p = held.position();
      delta = { x: p.x - from.x, y: p.y - from.y };
    }
    const pull = still.matches ? 1 : PULL, keep = still.matches ? 0 : KEEP;
    let energy = 0;
    for (; spare >= STEP; spare -= STEP) {
      energy = 0;
      for (const f of moving) {
        f.vx = (f.vx + (f.x + delta.x * f.weight - f.px) * pull) * keep;
        f.vy = (f.vy + (f.y + delta.y * f.weight - f.py) * pull) * keep;
        f.px += f.vx;
        f.py += f.vy;
        energy += Math.abs(f.vx) + Math.abs(f.vy);
      }
    }
    cy.batch(() => { for (const f of moving) f.node.position({ x: f.px, y: f.py }); });
    frame = held || energy > 0.02 * moving.length ? requestAnimationFrame(tick) : 0;
  };

  cy.on('grabon', 'node', (e) => {
    const n: NodeSingular = e.target;
    if (n.data('member')) return;
    held = n;
    from = { ...n.position() };
    delta = { x: 0, y: 0 };
    const pull = n.isParent() ? boxShares(n) : shares(n);
    // A class's methods and fields travel with it, and with any neighbour that follows.
    const members = (o: NodeSingular, weight: number) =>
      cy.nodes(`[owner = "${o.id()}"]`).forEach((m) => { pull.set(m, weight); });
    if (!n.isParent()) members(n, 1);
    for (const [o, weight] of [...pull]) if (!o.data('member')) members(o, weight);
    moving = [...pull].map(([node, weight]) => {
      const { x, y } = node.position();
      return { node, weight, x, y, px: x, py: y, vx: 0, vy: 0 };
    });
    if (!frame && moving.length) {
      last = performance.now();
      spare = 0;
      frame = requestAnimationFrame(tick);
    }
  });
  cy.on('freeon', 'node', (e) => {
    const n: NodeSingular = e.target;
    held = null;
    const box = n.isParent() ? n : n.parent().first();
    if (box.nonempty()) setTimeout(() => pushBoxesApart(cy, box as NodeSingular), still.matches ? 0 : 320);
  });
  cy.on('destroy', () => { cancelAnimationFrame(frame); });
}
