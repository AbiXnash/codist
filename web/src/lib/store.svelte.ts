import { api } from './api';
import type { AskMessage, CommitItem, Detail, FindingWithPath, FixProposal, Graph, IgnoreScope, Incident, IncidentRow, IssueList, Mode, OpenFile, Status, Tab } from './types';

export interface Toast {
  id: number;
  text: string;
  tone: 'info' | 'good' | 'warn' | 'bad';
}

export type Theme = 'light' | 'dark';

function initialTheme(): Theme {
  try {
    const saved = localStorage.getItem('codist.theme');
    if (saved === 'light' || saved === 'dark') return saved;
  } catch {
    /* storage unavailable */
  }
  return matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

function pref(key: string, def: boolean): boolean {
  try {
    const v = localStorage.getItem('codist.' + key);
    return v === null ? def : v === '1';
  } catch {
    return def;
  }
}

export function savePref(key: string, value: boolean) {
  try {
    localStorage.setItem('codist.' + key, value ? '1' : '0');
  } catch {
    /* storage unavailable */
  }
}

/** Global UI state. Large server payloads are $state.raw so they are not deep-proxied. */
class AppState {
  theme = $state<Theme>(initialTheme());
  status = $state.raw<Status | null>(null);
  graph = $state.raw<Graph | null>(null);
  commits = $state.raw<CommitItem[]>([]);
  detail = $state.raw<Detail | null>(null);
  file = $state.raw<OpenFile | null>(null);

  selectedSha = $state<string | null>(null);
  selectedNode = $state<string | null>(null);
  tab = $state<Tab>('review');
  search = $state('');
  live = $state(false);
  toasts = $state<Toast[]>([]);
  /** Line to scroll the code view to; the nonce re-triggers the same line. */
  focusLine = $state<{ line: number; nonce: number } | null>(null);

  /** The fix dialog: generating, showing a proposal for approval, or an error. */
  fix = $state.raw<{ finding: FindingWithPath; sha: string; busy: 'generating' | 'applying' | null; proposal: FixProposal | null; error: string | null } | null>(null);

  showIgnored = $state(false);
  mode = $state<Mode>('review');
  incidents = $state.raw<IncidentRow[]>([]);
  incident = $state.raw<Incident | null>(null);
  incidentMarkdown = $state('');
  incidentId = $state<string | null>(null); // null = "new analysis" form
  sidebar = $state<'commits' | 'issues'>('commits');
  issueState = $state<'open' | 'resolved' | 'ignored'>('open');
  issueList = $state.raw<IssueList | null>(null);
  ask = $state<AskMessage[]>([]);
  asking = $state(false);

  follow = $state(true);
  focusChanges = $state(true);
  groupPackages = $state(false);
  showTests = $state(false);

  // Layout: which areas are visible. Persisted per browser.
  showCommits = $state(pref('commits', true));
  showPanel = $state(pref('panel', true));
  showLegend = $state(pref('legend', true));

  allFindings: FindingWithPath[] = $derived(
    (this.detail?.review?.files ?? []).flatMap((f) => f.findings.map((x) => ({ ...x, path: f.path }))),
  );
  /** Findings that count: ignored ones are hidden everywhere except the "show ignored" list. */
  findings: FindingWithPath[] = $derived(this.allFindings.filter((f) => !f.ignored));
  /** One entry per issue introduced by this commit: findings with the same root cause are listed once, root first. */
  issues: FindingWithPath[][] = $derived(
    groupIssues((this.showIgnored ? this.allFindings : this.findings).filter((f) => !f.firstSeen || f.firstSeen === this.selectedSha)),
  );
  /** Issues this commit still shows but that an earlier commit introduced. */
  carried: FindingWithPath[][] = $derived(
    groupIssues(this.findings.filter((f) => f.firstSeen && f.firstSeen !== this.selectedSha)),
  );
}

const SEV_ORDER = ['critical', 'high', 'medium', 'low', 'info'];

function groupIssues(findings: FindingWithPath[]): FindingWithPath[][] {
  const groups = new Map<string, FindingWithPath[]>();
  findings.forEach((f, i) => {
    const key = f.group && !f.ignored ? f.group : `single-${i}`;
    groups.set(key, [...(groups.get(key) ?? []), f]);
  });
  const issues = [...groups.values()].map((g) => [...g].sort((a, b) => Number(!!b.groupRoot) - Number(!!a.groupRoot)));
  const worst = (g: FindingWithPath[]) => Math.min(...g.map((f) => SEV_ORDER.indexOf(f.severity)));
  return issues.sort((a, b) => worst(a) - worst(b) || b.length - a.length);
}

export const app = new AppState();

export function setTheme(t: Theme) {
  app.theme = t;
  document.documentElement.dataset.theme = t;
  try {
    localStorage.setItem('codist.theme', t);
  } catch {
    /* storage unavailable */
  }
}
document.documentElement.dataset.theme = app.theme;

let toastId = 0;
export function toast(text: string, tone: Toast['tone'] = 'info', ms = 4000) {
  const id = ++toastId;
  app.toasts = [...app.toasts, { id, text, tone }];
  setTimeout(() => (app.toasts = app.toasts.filter((t) => t.id !== id)), ms);
}

export async function loadStatus() {
  app.status = await api.status();
}

export async function loadGraph() {
  const prev = app.graph;
  app.graph = await api.graph();
  if (prev) {
    const delta = app.graph.nodes.length - prev.nodes.length;
    if (delta) toast(`Graph updated · ${delta > 0 ? '+' : ''}${delta} type${Math.abs(delta) === 1 ? '' : 's'}`);
  }
}

export async function loadCommits() {
  app.commits = await api.commits();
}

export async function loadIssues() {
  try {
    app.issueList = await api.issues(app.issueState);
  } catch {
    /* tracker unavailable until the first review */
  }
}

export async function selectCommit(sha: string) {
  app.selectedSha = sha;
  await refreshDetail();
  app.tab = 'review';
}

export async function refreshDetail() {
  const sha = app.selectedSha;
  if (!sha) return;
  try {
    const d = await api.commit(sha);
    if (app.selectedSha === sha) app.detail = d;
  } catch (e) {
    toast(`Could not load commit: ${(e as Error).message}`, 'bad');
  }
}

export async function requestReview(sha: string) {
  try {
    const r = await api.review(sha);
    toast(r.queued ? 'Review queued' : 'Already reviewing this commit');
  } catch (e) {
    toast((e as Error).message, 'bad');
  }
}

export async function ignoreFinding(f: FindingWithPath | FindingWithPath[], scope: IgnoreScope, team = false) {
  const sha = app.selectedSha;
  if (!sha) return;
  const members = Array.isArray(f) ? f : [f];
  // A rule ignore covers every place at once; commit and line ignores are added per place.
  const targets = scope === 'rule' ? members.filter((m, i) => m.ref && members.findIndex((x) => x.ref === m.ref) === i) : members;
  try {
    for (const m of targets) await api.ignore({ scope, sha, path: m.path, line: m.line, title: m.title, team });
    const where = { commit: 'in this commit', code: 'on this line in every commit', rule: 'everywhere' }[scope];
    toast(`Ignored ${where}${team ? ' (saved to .codist/ignore.yaml)' : ''}`, 'good');
    await Promise.all([refreshDetail(), loadCommits(), loadIssues()]);
  } catch (e) {
    toast((e as Error).message, 'bad');
  }
}

export async function unignore(id: string) {
  try {
    await api.unignore(id);
    await Promise.all([refreshDetail(), loadCommits()]);
  } catch (e) {
    toast((e as Error).message, 'bad');
  }
}

/** The file the Ask tab talks about: the open file, else the selected type's file. */
export function askTarget(): { path: string | null; node: string | null } {
  const node = app.selectedNode;
  const path = app.file?.path ?? app.graph?.nodes.find((n) => n.id === node)?.file ?? null;
  return { path, node };
}

export async function askQuestion(question: string) {
  const { path, node } = askTarget();
  app.ask = [...app.ask, { role: 'user', text: question, path }];
  app.asking = true;
  try {
    const { answer } = await api.ask({ path, question, node });
    app.ask = [...app.ask, { role: 'assistant', text: answer, path }];
  } catch (e) {
    app.ask = [...app.ask, { role: 'assistant', text: (e as Error).message, path, error: true }];
  } finally {
    app.asking = false;
  }
}

/** Turns an instruction into a proposed diff, reviewed in the same dialog as fixes. */
export async function requestEdit(instruction: string) {
  const { path } = askTarget();
  if (!path) {
    toast('Open a file or select a class first', 'warn');
    return;
  }
  const finding = { title: instruction.split('\n')[0], path, line: 0, endLine: 0, severity: 'info', category: 'edit', detail: instruction, suggestion: '', nodeId: '', onChangedLine: true, source: 'llm' } as FindingWithPath;
  app.fix = { finding, sha: app.selectedSha ?? '', busy: 'generating', proposal: null, error: null };
  try {
    const proposal = await api.edit(path, instruction);
    if (app.fix?.finding === finding) app.fix = { ...app.fix, busy: null, proposal };
  } catch (e) {
    if (app.fix?.finding === finding) app.fix = { ...app.fix, busy: null, error: (e as Error).message };
  }
}

export async function proposeFix(finding: FindingWithPath, fromSha?: string) {
  const sha = fromSha ?? app.selectedSha;
  if (!sha) return;
  app.fix = { finding, sha, busy: 'generating', proposal: null, error: null };
  try {
    const proposal = await api.proposeFix({ sha, path: finding.path, line: finding.line, title: finding.title });
    if (app.fix?.finding === finding) app.fix = { ...app.fix, busy: null, proposal };
  } catch (e) {
    if (app.fix?.finding === finding) app.fix = { ...app.fix, busy: null, error: (e as Error).message };
  }
}

export async function applyFix(message: string) {
  const f = app.fix;
  if (!f?.proposal) return;
  app.fix = { ...f, busy: 'applying', error: null };
  try {
    const { commit } = await api.applyFix(f.proposal.id, message);
    app.fix = null;
    toast(`Committed ${commit.slice(0, 7)} · reviewing the fix`, 'good', 5000);
    void loadIssues();
  } catch (e) {
    app.fix = { ...f, busy: null, error: (e as Error).message };
  }
}

/** Opens any server proposal (incident fix, regression test, runbook) in the approve-and-commit dialog. */
export async function openProposal(title: string, path: string, make: () => Promise<FixProposal>) {
  const finding = { title, path, line: 0, endLine: 0, severity: 'info', category: 'change', detail: '', suggestion: '', nodeId: '', onChangedLine: true, source: 'rule' } as FindingWithPath;
  app.fix = { finding, sha: app.selectedSha ?? '', busy: 'generating', proposal: null, error: null };
  try {
    const proposal = await make();
    if (app.fix?.finding === finding) app.fix = { ...app.fix, busy: null, proposal };
  } catch (e) {
    if (app.fix?.finding === finding) app.fix = { ...app.fix, busy: null, error: (e as Error).message };
  }
}

export async function loadIncidents() {
  try {
    app.incidents = await api.incidents();
  } catch {
    /* none yet */
  }
}

export async function openIncident(id: string | null) {
  app.incidentId = id;
  if (!id) {
    app.incident = null;
    return;
  }
  const d = await api.incident(id);
  if (app.incidentId === id) {
    app.incident = d.incident;
    app.incidentMarkdown = d.markdown;
  }
}

export function closeFix() {
  const id = app.fix?.proposal?.id;
  if (id) void api.discardFix(id);
  app.fix = null;
}

export function fileInCommit(path: string): boolean {
  return !!app.detail?.analysis.files.some((f) => f.newPath === path);
}

export async function openFile(path: string, sha: string | null, line: number | null = null, switchTab = true) {
  const key = `${path}@${sha ?? ''}`;
  if (app.file?.key !== key) {
    try {
      const { content } = await api.file(path, sha);
      app.file = { key, path, sha, content };
    } catch {
      toast(`Could not open ${path}`, 'bad');
      return;
    }
  }
  if (switchTab) app.tab = 'code';
  if (line) app.focusLine = { line, nonce: Math.random() };
}

/** Opens a type: shows its details and loads its file (at the selected commit when it changed there). */
export function selectNode(id: string | null) {
  app.selectedNode = id;
  if (!id) return;
  const node = app.graph?.nodes.find((n) => n.id === id);
  if (!node) return;
  app.tab = 'type';
  void openFile(node.file, fileInCommit(node.file) ? app.selectedSha : null, node.startLine, false);
}

export function connect() {
  const es = new EventSource('/api/events');
  es.addEventListener('hello', () => (app.live = true));
  es.onerror = () => (app.live = false);

  es.addEventListener('graph', async () => {
    await loadGraph();
    void loadIssues(); // edits can resolve issues
    const f = app.file;
    if (f && !f.sha) {
      app.file = null;
      await openFile(f.path, null, null, false);
    }
  });

  es.addEventListener('commits', async (e) => {
    const d = JSON.parse((e as MessageEvent).data) as { head: string; new: string[] };
    await Promise.all([loadStatus(), loadCommits()]);
    toast(`${d.new.length} new commit${d.new.length > 1 ? 's' : ''} on ${app.status?.tracking} · reviewing`, 'info', 5000);
    if (app.follow) await selectCommit(d.head);
  });

  es.addEventListener('ignores', async () => {
    await Promise.all([refreshDetail(), loadCommits(), loadIssues()]);
  });

  // The release screen reloads its checklist when anyone ticks an item.
  es.addEventListener('signoff', (e) => window.dispatchEvent(new CustomEvent('codist:signoff', { detail: JSON.parse((e as MessageEvent).data) })));

  es.addEventListener('incident', async (e) => {
    const d = JSON.parse((e as MessageEvent).data) as { id: string; status: string };
    await loadIncidents();
    if (d.id === app.incidentId) await openIncident(d.id);
    if (d.status === 'done') toast('Incident analysis ready', 'good');
  });

  es.addEventListener('fix-ready', async (e) => {
    const d = JSON.parse((e as MessageEvent).data) as { sha: string };
    if (d.sha === app.selectedSha) await refreshDetail();
  });

  es.addEventListener('review', async (e) => {
    const d = JSON.parse((e as MessageEvent).data) as { sha: string; status: CommitItem['status']; counts: CommitItem['counts'] };
    app.commits = app.commits.map((c) => (c.commit.sha === d.sha ? { ...c, status: d.status, counts: d.counts } : c));
    if (d.sha === app.selectedSha) await refreshDetail();
    const short = d.sha.slice(0, 7);
    if (d.status === 'done') {
      const total = Object.values(d.counts ?? {}).reduce((a, b) => a + (b ?? 0), 0);
      toast(total ? `Review of ${short} found ${total} new issue${total > 1 ? 's' : ''}` : `Review of ${short}: no new issues`, total ? 'warn' : 'good', 6000);
      await Promise.all([loadCommits(), loadIssues()]);
    } else if (d.status === 'failed') {
      toast(`Review of ${short} failed`, 'bad', 6000);
    }
  });
}

export async function boot() {
  await loadStatus();
  await Promise.all([loadGraph(), loadCommits(), loadIssues(), loadIncidents()]);
  if (app.commits.length) await selectCommit(app.commits[0].commit.sha);
  connect();
  setInterval(() => void loadStatus(), 30_000);
}
