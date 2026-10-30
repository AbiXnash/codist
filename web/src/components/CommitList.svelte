<script lang="ts">
  import { app, selectCommit } from '../lib/store.svelte';
  import { ago, SEVERITIES, SEVERITY_COLOR } from '../lib/theme';
  import type { CommitItem, Severity } from '../lib/types';

  let now = $state(Date.now());
  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 60_000);
    return () => clearInterval(t);
  });

  function summary(c: CommitItem): { total: number; worst: Severity | null } {
    let total = 0;
    let worst: Severity | null = null;
    for (const s of SEVERITIES) {
      const n = c.counts?.[s] ?? 0;
      total += n;
      if (n && !worst) worst = s;
    }
    return { total, worst };
  }
</script>

<ul>
  {#each app.commits as c (c.commit.sha)}
    {@const s = summary(c)}
    <li>
      <button class="item" class:selected={c.commit.sha === app.selectedSha} onclick={() => selectCommit(c.commit.sha)}>
        <span class="msg">{c.commit.message}</span>
        <span class="state">
          {#if c.status === 'running' || c.status === 'queued'}
            <span class="spinner" title={c.status === 'queued' ? 'Queued' : 'Reviewing'}></span>
          {:else if c.status === 'failed'}
            <span class="fail" title="Review failed">!</span>
          {:else if c.status === 'done' && s.total}
            <span class="issues" style:--c={SEVERITY_COLOR[s.worst!]} title="{s.total} new issue{s.total > 1 ? 's' : ''} (worst: {s.worst})">{s.total}</span>
          {:else if c.status === 'done'}
            <svg class="ok" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" aria-label="No new issues"><path d="m3.5 8.5 3 3 6-7"/></svg>
          {/if}
        </span>
        <span class="meta">
          <span class="mono sha">{c.commit.shortSha}</span>
          <span class="author">{c.commit.author}</span>
          {#if c.fixed}<span class="fixed" title="{c.fixed} earlier issue{c.fixed > 1 ? 's' : ''} fixed by this commit">✓{c.fixed}</span>{/if}
          {#if c.messageProblem}<span class="warnmsg" title="The commit message could be clearer">msg</span>{/if}
          <span class="time">{ago(c.commit.time, now)}</span>
        </span>
      </button>
    </li>
  {:else}
    <li class="empty">No commits yet.</li>
  {/each}
</ul>

<style>
  ul { list-style: none; margin: 0; padding: 4px; overflow: auto; flex: 1; }
  .item { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 2px 8px; width: 100%; padding: 7px 8px; border: 0; border-radius: var(--r); background: none; text-align: left; }
  .item:hover { background: var(--hover); }
  .item.selected { background: var(--accent-soft); }
  .msg { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); }
  .state { grid-row: span 2; display: grid; place-items: center; min-width: 22px; color: var(--text-3); }
  .meta { display: flex; align-items: center; gap: 8px; min-width: 0; font-size: 11.5px; color: var(--text-3); }
  .sha { font-size: 11px; color: var(--text-2); }
  .author { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .time { margin-left: auto; white-space: nowrap; }
  .fixed { color: var(--good); font-weight: 600; }
  .warnmsg { font-size: 10px; font-weight: 600; padding: 0 4px; border-radius: 3px; color: var(--warn); border: 1px solid color-mix(in srgb, var(--warn) 45%, transparent); }
  .issues { min-width: 20px; height: 18px; padding: 0 5px; display: grid; place-items: center; border-radius: 4px; font: 600 11px var(--font); color: var(--c); background: color-mix(in srgb, var(--c) 15%, transparent); }
  .ok { color: var(--good); }
  .fail { color: var(--bad); font-weight: 700; }
</style>
