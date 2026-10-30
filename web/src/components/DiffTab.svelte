<script lang="ts">
  import { app, openFile } from '../lib/store.svelte';

  let collapsed = $state<Record<string, boolean>>({});
  const files = $derived(app.detail?.analysis.files ?? []);

  function lineClass(l: string) {
    if (l.startsWith('@@')) return 'hunk';
    if (/^(\+\+\+|---|diff |index |new file|deleted file|similarity|rename )/.test(l)) return 'meta';
    if (l.startsWith('+')) return 'add';
    if (l.startsWith('-')) return 'del';
    return '';
  }
  const isCollapsed = (path: string) => collapsed[path] ?? files.length > 6;
</script>

{#if !app.detail}
  <div class="empty">Select a commit.</div>
{:else}
  <div class="diff">
    {#each files as f (f.path)}
      <div class="file">
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="file-head" onclick={() => (collapsed[f.path] = !isCollapsed(f.path))}>
          <svg class="chev" class:open={!isCollapsed(f.path)} width="10" height="10" viewBox="0 0 10 10" fill="currentColor"><path d="M3 1.5 7 5 3 8.5Z"/></svg>
          <span class="ct {f.changeType}" title={f.changeType}>{f.changeType[0]}</span>
          <span class="name mono" title={f.path}>{f.oldPath && f.newPath && f.oldPath !== f.newPath ? `${f.oldPath} → ${f.newPath}` : f.path}</span>
          <span class="plus mono">+{f.addedLines.length}</span>
          {#if f.newPath}
            <button class="btn secondary sm" onclick={(e) => { e.stopPropagation(); void openFile(f.newPath!, app.selectedSha); }}>Open</button>
          {/if}
        </div>
        {#if !isCollapsed(f.path)}
          <pre class="patch">{#each f.patch.split('\n') as l}<span class={lineClass(l)}>{l || ' '}</span>{/each}</pre>
        {/if}
      </div>
    {:else}
      <div class="empty">No file changes.</div>
    {/each}
  </div>
{/if}

<style>
  .diff { padding: 12px; }
  .file { margin-bottom: 8px; border: 1px solid var(--border); border-radius: var(--r-lg); overflow: hidden; }
  .file-head { display: flex; align-items: center; gap: 8px; height: 34px; padding: 0 8px; background: var(--surface-2); cursor: pointer; }
  .file-head:hover { background: var(--surface-3); }
  .chev { color: var(--text-3); transition: transform 0.15s var(--ease); flex-shrink: 0; }
  .chev.open { transform: rotate(90deg); }
  .name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ct { width: 18px; height: 18px; display: grid; place-items: center; border-radius: 4px; font: 600 10.5px var(--mono); }
  .ct.ADD { color: var(--added); background: color-mix(in srgb, var(--added) 15%, transparent); }
  .ct.DELETE { color: var(--bad); background: color-mix(in srgb, var(--bad) 15%, transparent); }
  .ct.MODIFY { color: var(--sev-medium); background: color-mix(in srgb, var(--sev-medium) 15%, transparent); }
  .ct.RENAME, .ct.COPY { color: var(--accent-text); background: var(--accent-soft); }
  .plus { color: var(--added); font-size: 11.5px; }
  .patch { margin: 0; overflow: auto; font: 12px/1.55 var(--mono); padding: 4px 0; background: var(--code-bg); border-top: 1px solid var(--border); }
  .patch span { display: block; padding: 0 12px; white-space: pre; min-width: max-content; }
  .add { background: color-mix(in srgb, var(--added) 13%, transparent); }
  .del { background: color-mix(in srgb, var(--bad) 12%, transparent); }
  .hunk { color: var(--accent-text); background: var(--accent-soft); }
  .meta { color: var(--text-3); }
</style>
