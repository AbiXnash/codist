// Places commits on lanes for a branch graph. Commits arrive children first. Each open lane waits for
// the commit it leads to, so a line never runs through a commit that does not belong to it.

export interface LaneCommit { sha: string; parents: string[] }
export interface Placed { lane: number; row: number }
export interface Edge { from: Placed; to: Placed; via: number; lane: number }
export interface Lanes { placed: Map<string, Placed>; edges: Edge[]; width: number }

export function layoutLanes(commits: LaneCommit[]): Lanes {
  const open: (string | null)[] = []; // what each lane is waiting for
  const placed = new Map<string, Placed>();
  const wanted: { from: string; to: string; via: number }[] = [];
  let width = 1;
  const free = () => {
    const i = open.indexOf(null);
    return i >= 0 ? i : open.push(null) - 1;
  };
  commits.forEach((c, row) => {
    let lane = open.indexOf(c.sha);
    if (lane < 0) lane = free();
    // Every other lane that waited for this commit ends here.
    open.forEach((sha, i) => { if (sha === c.sha) open[i] = null; });
    placed.set(c.sha, { lane, row });
    c.parents.forEach((p, i) => {
      let via = open.indexOf(p);
      if (via < 0) {
        via = i === 0 ? lane : free();
        open[via] = p;
      }
      wanted.push({ from: c.sha, to: p, via });
    });
    width = Math.max(width, open.length);
    while (open.length && open[open.length - 1] === null) open.pop();
  });
  const edges: Edge[] = [];
  for (const w of wanted) {
    const from = placed.get(w.from)!;
    const to = placed.get(w.to);
    // A parent past the end of the list still gets a line, running off the bottom.
    edges.push({ from, to: to ?? { lane: w.via, row: commits.length }, via: w.via, lane: w.via });
  }
  return { placed, edges, width };
}
