<script lang="ts">
  import { app, applyFix, closeFix, toast } from '../lib/store.svelte';
  import { api } from '../lib/api';
  import CopyButton from './CopyButton.svelte';

  let message = $state('');
  let view = $state<'split' | 'unified'>('split');
  let rejecting = $state(false);
  let reason = $state('');
  let lastId = '';
  $effect(() => {
    const p = app.fix?.proposal;
    if (p && p.id !== lastId) {
      lastId = p.id;
      message = p.message;
      rejecting = false;
      reason = '';
    }
  });

  // Keep a file header per file so multi-file fixes stay readable.
  const lines = $derived(
    (app.fix?.proposal?.patch ?? '').split('\n').flatMap((l) => {
      if (l.startsWith('+++ ')) return [`file ${l.slice(6)}`];
      return /^(diff |index |--- )/.test(l) ? [] : [l];
    }),
  );
  const stats = $derived({
    add: lines.filter((l) => l.startsWith('+')).length,
    del: lines.filter((l) => l.startsWith('-')).length,
  });

  // Old on the left, new on the right, with removed and added lines paired up.
  type Cell = { text: string; kind: '' | 'add' | 'del' | 'hunk' | 'fileh' | 'none' };
  const rows = $derived.by(() => {
    const out: [Cell, Cell][] = [];
    let dels: string[] = [], adds: string[] = [];
    const flush = () => {
      for (let i = 0; i < Math.max(dels.length, adds.length); i++) {
        out.push([
          i < dels.length ? { text: dels[i], kind: 'del' } : { text: '', kind: 'none' },
          i < adds.length ? { text: adds[i], kind: 'add' } : { text: '', kind: 'none' },
        ]);
      }
      dels = [];
      adds = [];
    };
    for (const l of lines) {
      if (l.startsWith('-')) dels.push(l.slice(1));
      else if (l.startsWith('+')) adds.push(l.slice(1));
      else {
        flush();
        if (l.startsWith('file ') || l.startsWith('@@')) {
          const c: Cell = { text: l, kind: l.startsWith('file ') ? 'fileh' : 'hunk' };
          out.push([c, c]);
        } else if (!l.startsWith('\\')) out.push([{ text: l.slice(1), kind: '' }, { text: l.slice(1), kind: '' }]);
      }
    }
    flush();
    return out;
  });

  // The model can change plenty and still leave the problem. It can't be proven here, but this can be seen.
  const flagged = $derived((app.fix?.finding.snippet ?? '').split('\n').map((l) => l.trim()).find((l) => l.length > 3) ?? '');
  const untouched = $derived(!!flagged && !lines.some((l) => l.startsWith('-') && l.includes(flagged)));

  // Saying why teaches later reviews and fixes; it is never required.
  async function reject() {
    const f = app.fix;
    if (!f) return;
    const text = reason.trim();
    closeFix();
    if (!text) return;
    try {
      await api.addMemory({ kind: 'rejected-fix', text, title: f.finding.title, ref: f.finding.ref, path: f.finding.path, snippet: f.finding.snippet });
      toast('Codist will remember that. See Memory.', 'good', 3000);
    } catch (e) {
      toast((e as Error).message, 'bad');
    }
  }

  function cls(l: string) {
    return l.startsWith('file ') ? 'fileh' : l.startsWith('@@') ? 'hunk' : l.startsWith('+') ? 'add' : l.startsWith('-') ? 'del' : '';
  }

  function key(e: KeyboardEvent) {
    if (!app.fix) return;
    if (e.key === 'Escape' && app.fix.busy !== 'applying') closeFix();
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && app.fix.proposal && !app.fix.busy) void applyFix(message);
  }
</script>

<svelte:window onkeydown={key} />

{#if app.fix}
  {@const f = app.fix}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="backdrop" onclick={() => { if (!f.busy && !f.proposal) closeFix(); }}></div>
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="fix-title">
    <header>
      <div>
        <h2 id="fix-title">Fix: {f.finding.title}</h2>
        <div class="sub mono">
          {#if f.proposal && f.proposal.paths.length > 1}{f.proposal.paths.length} files: {f.proposal.paths.map((p) => p.split('/').pop()).join(', ')}{:else}{f.finding.path}{f.finding.line ? ':' + f.finding.line : ''}{/if}
          {f.proposal ? (f.proposal.working ? ' · working tree, not committed' : ` · commit to ${f.proposal.branch}`) : ''}
        </div>
      </div>
      <button class="icon-btn" aria-label={f.busy === 'generating' ? 'Cancel' : 'Close'} title={f.busy === 'generating' ? 'Cancel and stop the model' : 'Close'} disabled={f.busy === 'applying'} onclick={closeFix}>
        <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="m4 4 8 8M12 4l-8 8"/></svg>
      </button>
    </header>

    <div class="body">
      {#if f.busy === 'generating'}
        <div class="state"><span class="spinner"></span>Generating a fix with {app.status?.model ?? 'the model'}… this takes 20–60 s.</div>
      {:else if f.proposal}
        {#if f.finding.detail || f.finding.suggestion}
          <section class="why">
            <span class="label">Why this change</span>
            {#if f.finding.detail}<p>{f.finding.detail}</p>{/if}
            {#if f.finding.suggestion}<p class="rec"><b>Recommendation:</b> {f.finding.suggestion}</p>{/if}
          </section>
        {/if}
        {#if untouched}
          <p class="warn"><b>The flagged line is unchanged:</b> <code>{flagged}</code>. Check that this really fixes the problem before applying it.</p>
        {/if}
        <div class="diff-head">
          <span class="label">Old and new</span>
          <span class="mono plus">+{stats.add}</span><span class="mono minus">−{stats.del}</span>
          <span class="grow"></span>
          <div class="seg">
            <button class:on={view === 'split'} onclick={() => (view = 'split')}>Side by side</button>
            <button class:on={view === 'unified'} onclick={() => (view = 'unified')}>Unified</button>
          </div>
          <CopyButton text={f.proposal.patch} title="Copy the patch" label="Patch" />
        </div>
        {#if view === 'split'}
          <div class="split-head"><span>Current</span><span>Proposed</span></div>
          <div class="split-diff">
            {#each rows as [o, n]}
              {#if o.kind === 'fileh' || o.kind === 'hunk'}
                <div class="wide {o.kind}">{o.text}</div>
              {:else}
                <div class="cell {o.kind}">{o.text || ' '}</div>
                <div class="cell {n.kind}">{n.text || ' '}</div>
              {/if}
            {/each}
          </div>
        {:else}
          <pre class="diff">{#each lines as l}<span class={cls(l)}>{l || ' '}</span>{/each}</pre>
        {/if}

        {#if f.proposal?.working}
          <p class="faint small">This fixes your uncommitted changes, so it is applied to the working tree and not committed. Commit it together with your changes.</p>
        {:else}
          <div class="lab"><label class="label" for="fix-msg">Commit message</label><CopyButton text={message} title="Copy the message" /></div>
          <textarea id="fix-msg" class="mono" rows="5" bind:value={message} spellcheck="false"></textarea>
        {/if}
      {/if}
      {#if f.error}<div class="err"><p class="error">{f.error}</p><CopyButton text={f.error} title="Copy the error" /></div>{/if}
      {#if rejecting}
        <div class="reject">
          <label class="label" for="fix-why">Why not? Optional</label>
          <textarea id="fix-why" rows="2" bind:value={reason} placeholder="e.g. we keep field injection in legacy controllers. Codist remembers this in later reviews and fixes."></textarea>
        </div>
      {/if}
    </div>

    <footer>
      <span class="faint hint">{f.proposal ? `⌘↵ to ${f.proposal.working ? 'apply' : 'commit'} · Esc to discard` : f.busy === 'generating' ? 'Cancel or Esc stops the model. Clicking outside does nothing.' : ''}</span>
      {#if f.proposal && rejecting}
        <button class="btn secondary" onclick={() => (rejecting = false)}>Back</button>
        <button class="btn" onclick={reject}>{reason.trim() ? 'Reject and remember' : 'Reject'}</button>
      {:else if f.proposal}
        <button class="btn secondary" disabled={f.busy === 'applying'} onclick={() => (rejecting = true)}>Reject…</button>
      {:else}
        <button class="btn secondary" disabled={f.busy === 'applying'} onclick={closeFix}>{f.busy === 'generating' ? 'Cancel' : 'Close'}</button>
      {/if}
      {#if f.proposal && !rejecting}
        <button class="btn" disabled={!!f.busy || (!f.proposal?.working && !message.trim())} onclick={() => applyFix(message)}>
          {#if f.busy === 'applying'}<span class="spinner"></span>Applying…{:else if f.proposal?.working}Apply to working tree{:else}Apply & commit{/if}
        </button>
      {/if}
    </footer>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 60; background: rgba(0, 0, 0, 0.45); }
  .dialog {
    position: fixed;
    z-index: 61;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(1120px, calc(100vw - 32px));
    max-height: calc(100vh - 64px);
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    background: var(--surface);
    box-shadow: var(--shadow);
  }
  header { display: flex; justify-content: space-between; gap: 12px; padding: 14px 16px 12px; border-bottom: 1px solid var(--border); }
  h2 { margin: 0; font-size: 14px; font-weight: 600; }
  .sub { margin-top: 2px; font-size: 11.5px; color: var(--text-3); }
  .body { padding: 14px 16px; overflow: auto; min-height: 0; }
  .state { display: flex; align-items: center; gap: 10px; padding: 24px 0; color: var(--text-2); }
  .why { margin-bottom: 12px; padding: 9px 12px; border: 1px solid var(--border); border-left: 3px solid var(--accent); border-radius: var(--r); background: var(--surface-2); }
  .why p { margin: 4px 0 0; color: var(--text-2); }
  .why .rec b { color: var(--text); font-weight: 600; }
  .warn { margin: 0 0 12px; padding: 8px 12px; border-radius: var(--r); color: var(--text); background: color-mix(in srgb, var(--warn) 13%, transparent); font-size: 12.5px; }
  .warn b { color: var(--warn); font-weight: 600; }
  .warn code { font: 11.5px var(--mono); overflow-wrap: anywhere; }
  .grow { flex: 1; }
  .seg { display: inline-flex; padding: 2px; border: 1px solid var(--border); border-radius: var(--r); background: var(--surface-2); }
  .seg button { height: 20px; padding: 0 8px; border: 0; border-radius: 3px; background: none; font-size: 11px; color: var(--text-3); }
  .seg button.on { color: var(--text); background: var(--surface-3); }
  .split-head { display: grid; grid-template-columns: 1fr 1fr; font-size: 11px; font-weight: 600; color: var(--text-2); padding: 0 12px 3px; }
  .split-diff { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); margin: 0 0 14px; max-height: 46vh; overflow: auto; border: 1px solid var(--border); border-radius: var(--r); background: var(--code-bg); font: 12px/1.55 var(--mono); }
  .cell { padding: 0 10px; white-space: pre-wrap; overflow-wrap: anywhere; min-width: 0; }
  .cell + .cell { border-left: 1px solid var(--border); }
  .cell.none { background: var(--surface-2); }
  .wide { grid-column: 1 / -1; padding: 0 10px; }
  .wide.fileh { padding-top: 3px; }
  .lab { display: flex; align-items: center; gap: 6px; margin-bottom: 6px; }
  .lab label { margin: 0; }
  .err { display: flex; align-items: flex-start; gap: 6px; }
  .err .error { flex: 1; }
  .reject { margin-top: 12px; }
  .diff-head { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; font-size: 12px; }
  .plus { color: var(--added); font-size: 12px; }
  .minus { color: var(--bad); font-size: 12px; }
  .diff {
    margin: 0 0 14px;
    max-height: 46vh;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: var(--r);
    background: var(--code-bg);
    font: 12px/1.55 var(--mono);
    padding: 4px 0;
  }
  .diff span { display: block; padding: 0 12px; white-space: pre; min-width: max-content; }
  .add { background: color-mix(in srgb, var(--added) 14%, transparent); }
  .del { background: color-mix(in srgb, var(--bad) 13%, transparent); }
  .hunk { color: var(--accent-text); background: var(--accent-soft); }
  .fileh { font-weight: 600; color: var(--text); background: var(--surface-3); border-top: 1px solid var(--border); padding-top: 3px !important; }
  label { display: block; margin-bottom: 6px; }
  textarea {
    width: 100%;
    resize: vertical;
    padding: 8px 10px;
    border: 1px solid var(--border-strong);
    border-radius: var(--r);
    background: var(--surface-2);
    color: var(--text);
    font-size: 12px;
    line-height: 1.5;
  }
  textarea:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
  .error { margin: 10px 0 0; color: var(--bad); }
  footer { display: flex; align-items: center; justify-content: flex-end; gap: 8px; padding: 12px 16px; border-top: 1px solid var(--border); }
  .hint { margin-right: auto; font-size: 11.5px; }
  .btn:disabled { opacity: 0.55; cursor: default; filter: none; }
</style>
