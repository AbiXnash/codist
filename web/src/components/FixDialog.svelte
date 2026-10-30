<script lang="ts">
  import { app, applyFix, closeFix } from '../lib/store.svelte';

  let message = $state('');
  let lastId = '';
  $effect(() => {
    const p = app.fix?.proposal;
    if (p && p.id !== lastId) {
      lastId = p.id;
      message = p.message;
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

  function cls(l: string) {
    return l.startsWith('file ') ? 'fileh' : l.startsWith('@@') ? 'hunk' : l.startsWith('+') ? 'add' : l.startsWith('-') ? 'del' : '';
  }

  function key(e: KeyboardEvent) {
    if (!app.fix) return;
    if (e.key === 'Escape' && !app.fix.busy) closeFix();
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && app.fix.proposal && !app.fix.busy) void applyFix(message);
  }
</script>

<svelte:window onkeydown={key} />

{#if app.fix}
  {@const f = app.fix}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="backdrop" onclick={() => { if (!f.busy) closeFix(); }}></div>
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="fix-title">
    <header>
      <div>
        <h2 id="fix-title">Fix: {f.finding.title}</h2>
        <div class="sub mono">
          {#if f.proposal && f.proposal.paths.length > 1}{f.proposal.paths.length} files: {f.proposal.paths.map((p) => p.split('/').pop()).join(', ')}{:else}{f.finding.path}{f.finding.line ? ':' + f.finding.line : ''}{/if}
          {f.proposal ? (f.proposal.working ? ' · working tree, not committed' : ` · commit to ${f.proposal.branch}`) : ''}
        </div>
      </div>
      <button class="icon-btn" aria-label="Close" disabled={!!f.busy} onclick={closeFix}>
        <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="m4 4 8 8M12 4l-8 8"/></svg>
      </button>
    </header>

    <div class="body">
      {#if f.busy === 'generating'}
        <div class="state"><span class="spinner"></span>Generating a fix with {app.status?.model ?? 'the model'}… this takes 20–60 s.</div>
      {:else if f.proposal}
        <div class="diff-head">
          <span class="label">Changes</span>
          <span class="mono plus">+{stats.add}</span><span class="mono minus">−{stats.del}</span>
          <span class="faint">{#if f.proposal.working}Check the diff before applying.{:else}Check the diff before committing. Only {f.proposal.paths.length > 1 ? 'these files are' : 'this file is'} committed.{/if}</span>
        </div>
        <pre class="diff">{#each lines as l}<span class={cls(l)}>{l || ' '}</span>{/each}</pre>

        {#if f.proposal?.working}
          <p class="faint small">This fixes your uncommitted changes, so it is applied to the working tree and not committed. Commit it together with your changes.</p>
        {:else}
          <label class="label" for="fix-msg">Commit message</label>
          <textarea id="fix-msg" class="mono" rows="5" bind:value={message} spellcheck="false"></textarea>
        {/if}
      {/if}
      {#if f.error}<p class="error">{f.error}</p>{/if}
    </div>

    <footer>
      <span class="faint hint">{f.proposal ? `⌘↵ to ${f.proposal.working ? 'apply' : 'commit'} · Esc to discard` : ''}</span>
      <button class="btn secondary" disabled={!!f.busy} onclick={closeFix}>{f.proposal ? 'Discard' : 'Close'}</button>
      {#if f.proposal}
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
    width: min(860px, calc(100vw - 32px));
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
