<script lang="ts">
  import { tick } from 'svelte';
  import { app } from '../lib/store.svelte';
  import { langFor, plain, tokenize, type Line } from '../lib/highlight';
  import { SEVERITIES } from '../lib/theme';
  import type { Severity } from '../lib/types';

  let scroller = $state<HTMLDivElement>();
  let lines = $state<Line[]>([]);
  let target = $state<number | null>(null);
  let showNotes = $state(true);

  $effect(() => {
    const f = app.file, theme = app.theme;
    if (!f) return;
    lines = plain(f.content);
    tokenize(f.content, langFor(f.path), theme).then((t) => { if (app.file === f && app.theme === theme) lines = t; });
  });

  // Decorations apply only when the open file is the version from the selected commit.
  const atCommit = $derived(!!app.file?.sha && app.file.sha === app.selectedSha);
  const change = $derived(atCommit ? app.detail?.analysis.files.find((x) => x.newPath === app.file?.path) : undefined);
  const added = $derived(new Set(change?.addedLines ?? []));
  const fileFindings = $derived(atCommit ? app.findings.filter((x) => x.path === app.file?.path) : []);
  const flags = $derived.by(() => {
    const m = new Map<number, Severity>();
    for (const x of fileFindings) {
      for (let l = x.line; l <= x.endLine; l++) {
        const cur = m.get(l);
        if (!cur || SEVERITIES.indexOf(x.severity) < SEVERITIES.indexOf(cur)) m.set(l, x.severity);
      }
    }
    return m;
  });
  const after = $derived.by(() => {
    const m = new Map<number, typeof fileFindings>();
    for (const x of fileFindings) m.set(x.endLine, [...(m.get(x.endLine) ?? []), x]);
    return m;
  });
  const marks = $derived([...new Set([...added, ...flags.keys()])].sort((a, b) => a - b));

  async function scrollTo(line: number) {
    await tick();
    target = line;
    scroller?.querySelector(`[data-line="${line}"]`)?.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }

  $effect(() => {
    const f = app.focusLine;
    if (f) void scrollTo(f.line);
  });

  function step(dir: 1 | -1) {
    if (!scroller || !marks.length) return;
    const cur = target ?? (dir === 1 ? 0 : Infinity);
    const next = dir === 1 ? marks.find((l) => l > cur) ?? marks[0] : [...marks].reverse().find((l) => l < cur) ?? marks[marks.length - 1];
    void scrollTo(next);
  }
</script>

{#if !app.file}
  <div class="empty">Open a class from the graph or a finding.</div>
{:else}
  <div class="wrap">
    <div class="head">
      <span class="path mono" title={app.file.path}>{app.file.path}</span>
      <span class="faint mono">{app.file.sha ? app.file.sha.slice(0, 7) : 'working tree'}</span>
      <span class="spacer"></span>
      {#if fileFindings.length}
        <button class="btn secondary sm" class:on={showNotes} onclick={() => (showNotes = !showNotes)} title="Show or hide findings inline">
          {showNotes ? 'Hide' : 'Show'} findings ({fileFindings.length})
        </button>
      {/if}
      {#if marks.length}
        <div class="nav">
          <button class="icon-btn" title="Previous change" aria-label="Previous change" onclick={() => step(-1)}>
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6"><path d="m3 7.5 3-3 3 3"/></svg>
          </button>
          <button class="icon-btn" title="Next change" aria-label="Next change" onclick={() => step(1)}>
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6"><path d="m3 4.5 3 3 3-3"/></svg>
          </button>
        </div>
      {/if}
    </div>

    <div class="scroller" bind:this={scroller}>
      <div class="code">
        {#each lines as line, i}
          {@const n = i + 1}
          <div class="row" class:added={added.has(n)} class:target={target === n} data-line={n} data-flag={flags.get(n)}>
            <span class="ln">{n}</span>
            <span class="lc">{#each line as t}<span style:color={t.color} class:i={t.fontStyle === 1}>{t.content}</span>{/each}{#if !line.length || (line.length === 1 && !line[0].content)}&nbsp;{/if}</span>
          </div>
          {#if showNotes}
            {#each after.get(n) ?? [] as x}
              <div class="note" style:--c="var(--sev-{x.severity})">
                <div class="note-head"><span class="sev {x.severity}">{x.severity}</span><b>{x.title}</b></div>
                {#if x.detail}<div class="note-detail">{x.detail}</div>{/if}
                {#if x.suggestion}<pre class="mono">{x.suggestion}</pre>{/if}
              </div>
            {/each}
          {/if}
        {/each}
      </div>
    </div>
  </div>
{/if}

<style>
  .wrap { display: flex; flex-direction: column; height: 100%; }
  .head { display: flex; align-items: center; gap: 10px; height: 38px; padding: 0 8px 0 14px; border-bottom: 1px solid var(--border); flex-shrink: 0; font-size: 12px; }
  .path { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; direction: rtl; text-align: left; min-width: 0; }
  .spacer { flex: 1; }
  .nav { display: flex; }
  .nav .icon-btn { width: 24px; height: 24px; }
  .scroller { flex: 1; overflow: auto; min-height: 0; background: var(--code-bg); container-type: inline-size; }
  .code { font: 12.5px/1.6 var(--mono); padding: 6px 0 60px; min-width: max-content; }
  .row { display: flex; white-space: pre; position: relative; }
  .ln { position: sticky; left: 0; width: 52px; flex-shrink: 0; padding-right: 14px; text-align: right; color: var(--text-3); user-select: none; background: var(--code-bg); }
  .lc { padding: 0 20px 0 14px; color: var(--text); }
  .i { font-style: italic; }

  .row.added { background: color-mix(in srgb, var(--added) 10%, transparent); }
  .row.added .ln { color: var(--added); background: color-mix(in srgb, var(--added) 10%, var(--code-bg)); box-shadow: inset -2px 0 0 var(--added); }
  .row[data-flag] { background: color-mix(in srgb, var(--sev-info) 12%, transparent); }
  .row[data-flag='critical'] { background: color-mix(in srgb, var(--sev-critical) 14%, transparent); }
  .row[data-flag='high'] { background: color-mix(in srgb, var(--sev-high) 13%, transparent); }
  .row[data-flag='medium'] { background: color-mix(in srgb, var(--sev-medium) 12%, transparent); }
  .row[data-flag='low'] { background: color-mix(in srgb, var(--sev-low) 11%, transparent); }
  .row.target { box-shadow: inset 0 0 0 1px var(--accent); }

  .note {
    position: sticky;
    left: 66px;
    width: min(620px, calc(100cqw - 86px));
    margin: 4px 0 8px 66px;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-left: 2px solid var(--c);
    border-radius: var(--r);
    background: var(--surface);
    font: 12px/1.5 var(--font);
    white-space: normal;
  }
  .note-head { display: flex; align-items: center; gap: 8px; }
  .note-detail { margin-top: 4px; color: var(--text-2); }
  .note pre { margin: 6px 0 0; padding: 6px 8px; border-radius: 4px; background: var(--surface-2); font-size: 11.5px; white-space: pre-wrap; }
</style>
