<script lang="ts">
  import { stereo } from '../lib/theme';
  let { stereotype }: { stereotype: string } = $props();
  const s = $derived(stereo(stereotype));
</script>

<span class="glyph" class:round={s.shape === 'ellipse' || s.shape === 'barrel'} title={s.label}>{s.glyph}</span>
