<script lang="ts">
  import { app, openFile, requestReview } from '../lib/store.svelte';
  import { ago, SEVERITIES } from '../lib/theme';
  import { langFor, tokenize, type Line } from '../lib/highlight';
  import FindingCard from './FindingCard.svelte';
  import CopyButton from './CopyButton.svelte';
  import type { FileChange, FindingWithPath } from '../lib/types';

  type Row = { kind: 'add' | 'del' | 'ctx' | 'hunk'; text: string; old?: number; now?: number };

  // Files in a very large commit start closed, so the page stays quick to scan.
  const MANY_FILES = 12;
  let closed = $state<Record<string, boolean>>({});
  let active = $state(0);

  const d = $derived(app.detail);
  const review = $derived(d?.review ?? null);
  const files = $derived(d?.analysis.files ?? []);
  const issues = $derived(app.issues);
  const done = $derived(review?.files.filter((f) => !f.pending).length ?? 0);
  const running = $derived(review?.status === 'running' || review?.status === 'queued');
  // Only a rough guide: recent files on this machine, times the files still waiting for the model.
  const timeLeft = $derived.by(() => {
    const waiting = review?.files.filter((f) => f.pending).length ?? 0;
    if (!review?.fileMs || !waiting) return '';
    const min = Math.ceil((review.fileMs * waiting) / 60000);
    return `about ${min} min left`;
  });
  const worstOf = (g: FindingWithPath[]) => SEVERITIES.find((s) => g.some((f) => f.severity === s)) ?? 'info';
  // Low and info findings, and claims Codist could not back, wait behind one button so serious ones stand out.
  const isNote = (g: FindingWithPath[]) => worstOf(g) === 'low' || worstOf(g) === 'info';
  const noteCount = $derived(issues.filter(isNote).length);
  let showNotes = $state(false);
  const counts = $derived(SEVERITIES.map((s) => [s, issues.filter((g) => worstOf(g) === s && !isNote(g)).length] as const).filter(([, n]) => n));
  // The order j and k walk through, which is the order issues appear on the page.
  const order = $derived(files.flatMap((f) => inFile(f.path).sort((a, b) => a[0].line - b[0].line)));

  function inFile(path: string) {
    return issues.filter((g) => g[0].path === path && (showNotes || !isNote(g)));
  }
  const isClosed = (path: string) => closed[path] ?? files.length > MANY_FILES;

  // Very long files stay plain, so opening a large commit doesn't stall the page.
  const MAX_HIGHLIGHT = 200_000;
  const allRows = $derived(new Map(files.map((f) => [f.path, rowsOf(f)])));
  let colours = $state<Record<string, Line[]>>({});
  $effect(() => {
    const theme = app.theme;
    const rows = allRows;
    colours = {};
    for (const [path, list] of rows) {
      const lang = langFor(path);
      // Hunk headers become blank lines, so each row keeps the same index as its coloured line.
      const code = list.map((r) => (r.kind === 'hunk' ? '' : r.text)).join('\n');
      if (!lang || code.length > MAX_HIGHLIGHT) continue;
      void tokenize(code, lang, theme).then((lines) => {
        if (allRows === rows && app.theme === theme) colours[path] = lines;
      });
    }
  });

  function rowsOf(f: FileChange): Row[] {
    const rows: Row[] = [];
    let old = 0;
    let now = 0;
    for (const line of f.patch.split('\n')) {
      const hunk = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)/);
      if (hunk) {
        old = Number(hunk[1]);
        now = Number(hunk[2]);
        rows.push({ kind: 'hunk', text: line });
      } else if (/^(diff |index |--- |\+\+\+ |new file|deleted file|similarity|rename |\\ )/.test(line)) {
        continue;
      } else if (line.startsWith('+')) {
        rows.push({ kind: 'add', text: line.slice(1), now: now++ });
      } else if (line.startsWith('-')) {
        rows.push({ kind: 'del', text: line.slice(1), old: old++ });
      } else if (rows.length) {
        rows.push({ kind: 'ctx', text: line.slice(1), old: old++, now: now++ });
      }
    }
    while (rows.length && rows[rows.length - 1].kind === 'ctx' && !rows[rows.length - 1].text) rows.pop();
    return rows;
  }
  const lastLine = (rows: Row[]) => {
    const seen = new Set(rows.flatMap((r) => (r.now ? [r.now] : [])));
    return (g: FindingWithPath[]) => seen.has(g[0].line);
  };
  const isActive = (g: FindingWithPath[]) => order[active] === g;

  function move(by: number) {
    if (!order.length) return;
    active = Math.max(0, Math.min(order.length - 1, active + by));
    const g = order[active];
    closed[g[0].path] = false;
    requestAnimationFrame(() => {
      // Only the page moves: scrolling the card into view would also shift the code sideways.
      const el = document.getElementById(`issue-${g[0].path}-${g[0].line}`);
      const page = el?.closest('.review');
      if (!el || !page) return;
      const top = el.getBoundingClientRect().top - page.getBoundingClientRect().top;
      page.scrollTo({ top: page.scrollTop + top - page.clientHeight / 2 });
    });
  }
  function keys(e: KeyboardEvent) {
    if (app.fix || e.metaKey || e.ctrlKey || e.altKey || (e.target as Element | null)?.closest?.('input, textarea, select')) return;
    if (e.key === 'j') move(1);
    if (e.key === 'k') move(-1);
  }
</script>

<svelte:window onkeydown={keys} />

<section class="review" aria-label="Changes in this commit">
  {#if !d}
    <div class="empty">Select a commit to review its changes.</div>
  {:else}
    <header>
      <h2>{d.analysis.commit.message || 'Commit'}</h2>
      <p class="meta">
        <span>{d.analysis.commit.author}</span>
        <span>{ago(d.analysis.commit.time)}</span>
        <span class="mono">{d.analysis.commit.shortSha}</span>
        <CopyButton text={d.analysis.commit.sha} title="Copy the commit id" />
      </p>
      <div class="state" role="status">
        {#if running}
          <span class="spinner"></span>
          <span>Reviewing, {done} of {files.length} files done{#if timeLeft}, {timeLeft}{/if}</span>
        {:else if !review}
          <span class="muted">Not reviewed yet</span>
          <button class="btn sm" onclick={() => requestReview(d.analysis.commit.sha)}>Review this commit</button>
        {:else if review.status === 'failed'}
          <span class="bad">The review failed: {review.error ?? 'no reason given'}</span>
          <button class="btn secondary sm" onclick={() => requestReview(d.analysis.commit.sha)}>Review again</button>
        {:else if counts.length || noteCount}
          {#each counts as [s, n]}<span class="sev {s}">{n} {s}</span>{/each}
          {#if noteCount}<button class="btn secondary sm" aria-pressed={showNotes} title="Low and info findings, and claims Codist could not confirm in the code" onclick={() => (showNotes = !showNotes)}>{showNotes ? 'Hide' : 'Show'} {noteCount} {noteCount === 1 ? 'note' : 'notes'}</button>{/if}
          <span class="faint"><kbd>j</kbd> <kbd>k</kbd> to move between issues</span>
        {:else}
          <span class="good">No issues found</span>
        {/if}
      </div>
    </header>

    {#each files as f (f.path)}
      {@const rows = allRows.get(f.path) ?? []}
      {@const here = inFile(f.path)}
      {@const inDiff = lastLine(rows)}
      <article class="file">
        <div class="head">
          <button class="toggle" aria-expanded={!isClosed(f.path)} onclick={() => (closed[f.path] = !isClosed(f.path))}>
            <svg class="chev" class:open={!isClosed(f.path)} width="10" height="10" viewBox="0 0 10 10" fill="currentColor" aria-hidden="true"><path d="M3 1.5 7 5 3 8.5Z"/></svg>
            <span class="path mono" title={f.path}>{f.oldPath && f.newPath && f.oldPath !== f.newPath ? `${f.oldPath} to ${f.newPath}` : f.path}</span>
          </button>
          {#if here.length}<span class="n">{here.length} {here.length === 1 ? 'issue' : 'issues'}</span>{/if}
          <span class="plus mono">+{f.addedLines.length}</span>
          {#if f.newPath}<button class="btn secondary sm" onclick={() => openFile(f.newPath!, app.selectedSha)}>Open file</button>{/if}
        </div>
        {#if !isClosed(f.path)}
          {#each here.filter((g) => !inDiff(g)) as g (g[0].title + g[0].line)}
            <div class="note" class:active={isActive(g)} id={`issue-${g[0].path}-${g[0].line}`}><FindingCard f={g[0]} members={g} showPath={false} /></div>
          {/each}
          {#if f.binary}
            <p class="faint pad">Binary file, so there are no lines to show.</p>
          {:else}
            <div class="code">
              {#each rows as r, i}
                {#if r.kind === 'hunk'}
                  <div class="row hunk"><span class="no"></span><span class="no"></span><code>{r.text}</code></div>
                {:else}
                  <div class="row {r.kind}"><span class="no">{r.old ?? ''}</span><span class="no">{r.now ?? ''}</span><code>{#if colours[f.path]?.[i]}{#each colours[f.path][i] as t}<span style:color={t.color}>{t.content}</span>{/each}{:else}{r.text || ' '}{/if}</code></div>
                  {#each r.now ? here.filter((g) => g[0].line === r.now) : [] as g (g[0].title + g[0].line)}
                    <div class="note {worstOf(g)}" class:active={isActive(g)} id={`issue-${g[0].path}-${g[0].line}`}><FindingCard f={g[0]} members={g} showPath={false} /></div>
                  {/each}
                {/if}
              {/each}
            </div>
          {/if}
        {/if}
      </article>
    {:else}
      <div class="empty">This commit changes no files.</div>
    {/each}
  {/if}
</section>

<style>
  .review { min-width: 0; min-height: 0; overflow: auto; padding: 18px 20px 64px; background: var(--bg); display: flex; flex-direction: column; gap: 14px; }
  header { display: grid; gap: 4px; max-width: 820px; }
  h2 { margin: 0; font-size: 18px; font-weight: 600; letter-spacing: -0.012em; line-height: 1.3; text-wrap: balance; }
  .meta { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px; margin: 0; color: var(--text-2); font-size: 12.5px; }
  .state { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 8px; min-height: 26px; margin-top: 4px; font-size: 12.5px; }
  .state .faint { margin-left: 6px; }
  .good { color: var(--good); font-weight: 500; }
  .bad { color: var(--bad); }
  .file { flex-shrink: 0; container-type: inline-size; border-radius: var(--r-lg); background: var(--surface); box-shadow: 0 0 0 1px var(--border); overflow: clip; }
  .head { position: sticky; top: -18px; z-index: 2; display: flex; align-items: center; gap: 10px; min-height: 36px; padding: 0 8px 0 4px; background: var(--surface-2); border-bottom: 1px solid var(--border); }
  .toggle { flex: 1; min-width: 0; display: flex; align-items: center; gap: 7px; height: 36px; padding: 0 6px; border: 0; background: none; text-align: left; }
  .chev { flex-shrink: 0; color: var(--text-3); transition: transform 0.12s var(--ease); }
  .chev.open { transform: rotate(90deg); }
  .path { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; direction: rtl; text-align: left; }
  .n { font-size: 12px; color: var(--mark); font-weight: 500; white-space: nowrap; }
  .plus { font-size: 11.5px; color: var(--added); }
  .pad { margin: 0; padding: 12px 14px; }
  .code { overflow-x: auto; font: 12px/1.6 var(--mono); font-variant-ligatures: none; background: var(--code-bg); }
  .row { display: grid; grid-template-columns: 44px 44px minmax(max-content, 1fr); width: max-content; min-width: 100%; }
  .row code { padding: 0 14px 0 10px; white-space: pre; font: inherit; }
  .no { padding-right: 8px; color: var(--text-3); text-align: right; user-select: none; font-size: 11px; font-variant-numeric: tabular-nums; line-height: 19.2px; }
  .row.add { background: color-mix(in srgb, var(--added) 12%, transparent); }
  .row.add .no { background: color-mix(in srgb, var(--added) 10%, transparent); }
  .row.del { background: color-mix(in srgb, var(--bad) 11%, transparent); }
  .row.del .no { background: color-mix(in srgb, var(--bad) 9%, transparent); }
  .row.hunk { color: var(--accent-text); background: var(--accent-soft); }
  /* The issue sits directly under its line, tied to it by a stripe in the colour of its severity. */
  .note { position: sticky; left: 0; width: min(calc(100cqw - 104px), 860px); margin: 2px 0 6px 88px; font-family: var(--font); border-left: 3px solid var(--note, var(--border-strong)); border-radius: 0 var(--r) var(--r) 0; background: var(--surface); box-shadow: 0 0 0 1px var(--border); scroll-margin: 60px; }
  .note.critical { --note: var(--sev-critical); }
  .note.high { --note: var(--sev-high); }
  .note.medium { --note: var(--sev-medium); }
  .note.low, .note.info { --note: var(--sev-low); }
  .note.active { box-shadow: 0 0 0 2px var(--mark); }
  .file > .note { margin: 8px 12px; width: auto; }
  @media (max-width: 1000px) { .review { padding: 14px 12px 40px; } .note { margin-left: 8px; width: calc(100cqw - 24px); } .row { grid-template-columns: 34px 34px minmax(max-content, 1fr); } }
  @media (prefers-reduced-motion: reduce) { .chev { transition: none; } }
</style>
