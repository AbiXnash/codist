<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { app, followBranch, saveStr, selectCommit, toast } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import { ago } from '../lib/theme';
  import { layoutLanes } from '../lib/branchLanes';
  import CopyButton from './CopyButton.svelte';
  import type { BranchRef, HistoryCommit } from '../lib/types';

  // Every branch, or the ones picked, as lanes. Click a commit to review it; click a branch name to
  // make the whole of Review follow that branch.
  const ROW = 32, LANE = 15, PAD = 12;
  const COLORS = ['#6366f1', '#06b6d4', '#22c55e', '#f59e0b', '#ec4899', '#8b5cf6', '#14b8a6', '#ef4444'];
  const color = (lane: number) => COLORS[lane % COLORS.length];

  let refs = $state.raw<BranchRef[]>([]);
  let commits = $state.raw<HistoryCommit[]>([]);
  let picked = $state<string[]>([]);
  let loading = $state(true);

  async function load() {
    try {
      [refs, commits] = await Promise.all([api.refs(), api.history(picked)]);
    } catch (e) {
      toast((e as Error).message, 'bad');
    } finally {
      loading = false;
    }
  }
  onMount(() => void load());
  $effect(() => { void app.status?.head; untrack(load); });

  function toggle(name: string) {
    picked = picked.includes(name) ? picked.filter((n) => n !== name) : [...picked, name];
    void load();
  }
  const lanes = $derived(layoutLanes(commits));
  const x = (lane: number) => PAD + lane * LANE;
  const y = (row: number) => ROW / 2 + row * ROW;
  const gutter = $derived(PAD * 2 + (lanes.width - 1) * LANE);

  // Leave the commit on its reserved lane, run straight down it, and join the parent at the end.
  function path(e: (typeof lanes.edges)[number]): string {
    const x1 = x(e.from.lane), y1 = y(e.from.row), v = x(e.via), x2 = x(e.to.lane), y2 = y(e.to.row);
    let d = `M${x1} ${y1}`;
    if (e.via !== e.from.lane) d += ` C${x1} ${y1 + ROW * 0.6} ${v} ${y1 + ROW * 0.4} ${v} ${y1 + ROW}`;
    const end = e.via !== e.to.lane ? y2 - ROW : y2;
    d += ` L${v} ${Math.max(end, e.via !== e.from.lane ? y1 + ROW : y1)}`;
    if (e.via !== e.to.lane) d += ` C${v} ${y2 - ROW * 0.4} ${x2} ${y2 - ROW * 0.6} ${x2} ${y2}`;
    return d;
  }
  const followable = $derived(new Set(refs.map((r) => r.name)));
  function open(c: HistoryCommit) {
    app.showPanel = true;
    app.tab = 'review';
    void selectCommit(c.sha, true);
  }
  const showCode = () => { app.centre = 'code'; saveStr('centre', 'code'); };
</script>

<section class="wrap">
  <header>
    <div class="seg">
      <button onclick={showCode}>Code graph</button>
      <button class="on">Branch graph</button>
    </div>
    <div class="chips" role="group" aria-label="Branches to show">
      <button class="chip" class:on={!picked.length} onclick={() => { picked = []; void load(); }}>All branches</button>
      {#each refs as r (r.name)}
        <button class="chip mono" class:on={picked.includes(r.name)} title="Show or hide {r.name}" onclick={() => toggle(r.name)}>{r.name}</button>
      {/each}
    </div>
    <span class="hint faint">Click a commit to review it, a branch name to follow it</span>
  </header>

  <div class="scroll">
    {#if loading && !commits.length}<div class="empty"><span class="spinner"></span></div>
    {:else if !commits.length}<div class="empty">No commits.</div>
    {:else}
      <div class="rows" style:height="{commits.length * ROW}px" style:--gutter="{gutter}px" style:--row="{ROW}px">
        <svg width={gutter} height={commits.length * ROW} aria-hidden="true">
          {#each lanes.edges as e}<path d={path(e)} fill="none" stroke={color(e.via)} stroke-width="1.8" />{/each}
          {#each commits as c (c.sha)}
            {@const p = lanes.placed.get(c.sha)!}
            <circle cx={x(p.lane)} cy={y(p.row)} r={c.parents.length > 1 ? 5.5 : 4} fill={c.status === 'done' ? color(p.lane) : 'var(--canvas)'} stroke={color(p.lane)} stroke-width="2" />
          {/each}
        </svg>
        {#each commits as c (c.sha)}
          {@const p = lanes.placed.get(c.sha)!}
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
          <div class="row" class:on={c.sha === app.selectedSha} onclick={() => open(c)}>
            {#each c.refs as ref}
              {#if followable.has(ref)}
                <button class="ref mono" class:cur={ref === app.status?.branch} style:--c={color(p.lane)} title="Follow {ref}: commits, code graph and issues all switch to it" onclick={(e) => { e.stopPropagation(); void followBranch(ref); }}>{ref}</button>
              {:else}
                <span class="ref mono" style:--c={color(p.lane)}>{ref}</span>
              {/if}
            {/each}
            <span class="msg" title={c.subject}>{c.subject}</span>
            {#if c.issues}<span class="n" title="Findings from its review">{c.issues}</span>{/if}
            <span class="who">{c.author}</span>
            <span class="sha mono">{c.sha.slice(0, 7)}</span>
            <CopyButton text={c.sha} title="Copy the commit id" />
            <span class="when">{ago(c.at)}</span>
          </div>
        {/each}
      </div>
      {#if commits.length >= 300}<p class="faint more">Showing the latest 300 commits.</p>{/if}
    {/if}
  </div>
</section>

<style>
  .wrap { position: relative; display: flex; flex-direction: column; min-width: 0; min-height: 360px; overflow: hidden; background: var(--canvas); }
  header { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 10px; border-bottom: 1px solid var(--border); }
  .seg { display: flex; padding: 2px; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--surface-2); }
  .seg button { height: 24px; padding: 0 10px; border: 0; border-radius: var(--r); background: none; font-size: 12px; color: var(--text-3); }
  .seg button.on { color: var(--text); background: var(--surface-3); box-shadow: 0 0 0 1px var(--border-strong); }
  .chips { display: flex; flex-wrap: wrap; gap: 4px; }
  .chip { height: 24px; padding: 0 9px; border: 1px solid var(--border); border-radius: 12px; background: none; color: var(--text-3); font-size: 11.5px; }
  .chip.on { color: var(--accent-text); border-color: var(--accent); background: var(--accent-soft); }
  .hint { margin-left: auto; font-size: 11.5px; }
  .scroll { flex: 1; min-height: 0; overflow: auto; }
  .rows { position: relative; min-width: 620px; }
  svg { position: absolute; left: 0; top: 0; pointer-events: none; z-index: 1; }
  .row { display: flex; align-items: center; gap: 8px; height: var(--row); padding: 0 10px 0 calc(var(--gutter) + 6px); font-size: 12.5px; cursor: pointer; }
  .row:hover { background: var(--hover); }
  .row.on { background: var(--accent-soft); }
  .msg { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ref { flex-shrink: 0; max-width: 190px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; height: 19px; line-height: 17px; padding: 0 6px; border: 1px solid var(--c); border-radius: 4px; background: color-mix(in srgb, var(--c) 14%, transparent); color: var(--text); font-size: 10.5px; }
  button.ref:hover { background: color-mix(in srgb, var(--c) 30%, transparent); }
  .ref.cur { font-weight: 700; background: color-mix(in srgb, var(--c) 34%, transparent); }
  .n { flex-shrink: 0; min-width: 18px; padding: 0 5px; border-radius: 9px; line-height: 17px; text-align: center; font-size: 11px; color: var(--warn); background: color-mix(in srgb, var(--warn) 15%, transparent); }
  .who, .when { flex-shrink: 0; font-size: 11.5px; color: var(--text-3); white-space: nowrap; }
  .who { max-width: 110px; overflow: hidden; text-overflow: ellipsis; }
  .sha { flex-shrink: 0; font-size: 11px; color: var(--text-3); }
  .when { width: 54px; text-align: right; }
  .more { padding: 10px; text-align: center; }
  @media (max-width: 1000px) { .wrap { height: 60vh; } }
</style>
