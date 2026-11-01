import type { Severity } from './types';

/** Class roles are told apart by shape and a letter, not by colour: colour is reserved for state. */
export const STEREOTYPE: Record<string, { label: string; glyph: string; shape: string }> = {
  controller: { label: 'Controller', glyph: 'C', shape: 'round-rectangle' },
  service: { label: 'Service', glyph: 'S', shape: 'ellipse' },
  repository: { label: 'Repository', glyph: 'R', shape: 'barrel' },
  entity: { label: 'Entity', glyph: 'E', shape: 'cut-rectangle' },
  dto: { label: 'DTO', glyph: 'D', shape: 'round-tag' },
  config: { label: 'Config', glyph: 'K', shape: 'hexagon' },
  component: { label: 'Component', glyph: 'B', shape: 'octagon' },
  client: { label: 'Feign client', glyph: 'F', shape: 'diamond' },
  advice: { label: 'Advice', glyph: 'A', shape: 'round-triangle' },
  exception: { label: 'Exception', glyph: '!', shape: 'triangle' },
  other: { label: 'Other', glyph: '·', shape: 'ellipse' },
  test: { label: 'Test', glyph: 'T', shape: 'ellipse' },
};

/** Rules with a mechanical fix: the Fix button opens instantly, no model involved. */
export const INSTANT_REFS = new Set(['console-output', 'literal-instead-of-constant', 'duplicate-log', 'log-helper-method', 'uninjected-dependency', 'copy-paste-wrong-constant', 'field-injection']);

/** Heads-ups that ask you to check something; "fixing" them would only undo your change. */
export const NOT_FIXABLE = new Set(['shared-constant-changed']);

export const SEVERITIES: Severity[] = ['critical', 'high', 'medium', 'low', 'info'];

export const SEVERITY_COLOR: Record<Severity, string> = {
  critical: '#ff453a',
  high: '#ff9f0a',
  medium: '#e6b800',
  low: '#8e8e93',
  info: '#aeaeb2',
};

export const STATE_COLOR = { changed: '#d97706', added: '#16a34a', impacted: '#ea580c', accent: '#2563eb', issue: '#dc2626' };

export function stereo(s: string) {
  return STEREOTYPE[s] ?? STEREOTYPE.other;
}

export function worst(list: { severity: Severity }[]): Severity {
  return list.reduce<Severity>((w, x) => (SEVERITIES.indexOf(x.severity) < SEVERITIES.indexOf(w) ? x.severity : w), 'info');
}

export function ago(ms: number, now = Date.now()): string {
  const s = Math.max(0, (now - ms) / 1000);
  if (s < 45) return 'just now';
  if (s < 3600) return `${Math.round(s / 60)}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  if (s < 86400 * 30) return `${Math.round(s / 86400)}d ago`;
  return new Date(ms).toLocaleDateString();
}

export const shortName = (id: string) => id.slice(id.lastIndexOf('.') + 1);

/** Reads a CSS custom property from :root, for canvas code (the graph) that cannot use var(). */
export function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}
