<script lang="ts">
  import { fly } from 'svelte/transition';
  import { app } from '../lib/store.svelte';
</script>

<div class="toasts" aria-live="polite">
  {#each app.toasts as t (t.id)}
    <div class="toast {t.tone}" in:fly={{ y: 8, duration: 160 }} out:fly={{ y: 4, duration: 120 }}>
      <span class="dot"></span>{t.text}
    </div>
  {/each}
</div>

<style>
  .toasts { position: fixed; right: 16px; bottom: 16px; display: flex; flex-direction: column; align-items: flex-end; gap: 6px; z-index: 100; pointer-events: none; }
  .toast {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border: 1px solid var(--border-strong);
    border-radius: var(--r-lg);
    background: var(--surface);
    box-shadow: var(--shadow);
    font-size: 12.5px;
  }
  .dot { width: 7px; height: 7px; background: var(--accent); }
  .good .dot { background: var(--good); }
  .warn .dot { background: var(--warn); }
  .bad .dot { background: var(--bad); }
</style>
