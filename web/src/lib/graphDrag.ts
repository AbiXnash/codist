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
  cy.on('freeon', 'node', () => { held = null; });
  cy.on('destroy', () => { cancelAnimationFrame(frame); });
}
