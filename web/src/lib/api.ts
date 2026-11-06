import type { FixCheck, CallLink, ImpactReport, Activity, TeamReport, Flags, PeopleReport, HistoryCommit, BranchRef, BranchReport, MemoryItem, MemoryReport, PlannedCommit, RepoReport, FixRef, Working, CommitItem, DependencyReport, Detail, FixProposal, Graph, Incident, IncidentRow, IssueList, RcaReport, ReleaseReport, Status } from './types';

const json = (method: string, body?: unknown): RequestInit => ({
  method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body),
});

// Requests that wait for the model: Cancel aborts them, and the server stops the model too.
const AI_PATH = /^\/(fix|edit|tests|working\/message|commits\/[^/]+\/message|incidents\/[^/]+\/(fix|test|rule|runbook))$/;
let aiAbort: AbortController | null = null;

export function cancelAI() {
  aiAbort?.abort();
  aiAbort = null;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  if (AI_PATH.test(path) && init?.method === 'POST') {
    aiAbort ??= new AbortController();
    init = { ...init, signal: aiAbort.signal };
  }
  const res = await fetch('/api' + path, init);
  if (!res.ok) {
    let message = res.statusText;
    try {
      message = (await res.json()).error ?? message;
    } catch {
      /* not JSON */
    }
    throw new Error(message);
  }
  return res.json() as Promise<T>;
}

export const api = {
  status: () => request<Status>('/status'),
  features: () => request<Flags>('/features'),
  people: () => request<PeopleReport>('/people'),
  team: () => request<TeamReport>('/team'),
  impact: (sha: string) => request<ImpactReport>(`/commits/${sha}/impact`),
  refs: () => request<BranchRef[]>('/refs'),
  history: (branches: string[]) => request<HistoryCommit[]>(`/history${branches.length ? `?branches=${encodeURIComponent(branches.join(','))}` : ''}`),
  follow: (name: string) => request<{ branch: string }>('/refs/follow', json('POST', { name })),
  repo: () => request<RepoReport>('/repo'),
  branches: () => request<BranchReport>('/branches'),
  activity: () => request<Activity[]>('/activity'),
  activityItem: (id: string) => request<Activity>(`/activity/${id}`),
  memory: () => request<MemoryReport>('/memory'),
  addMemory: (item: Partial<MemoryItem>) => request<MemoryItem>('/memory', json('POST', item)),
  removeMemory: (id: string) => fetch(`/api/memory/${id}`, { method: 'DELETE' }),
  shareMemory: (id: string, team: boolean) => fetch(`/api/memory/${id}/share`, json('POST', { team })),
  plan: () => request<PlannedCommit[]>('/working/plan'),
  planMessage: (files: string[]) => request<{ message: string; why: string }>('/working/message', json('POST', { files })),
  commitPaths: (files: string[], message: string) => request<{ commit: string }>('/working/commit', json('POST', { files, message })),
  betterMessage: (sha: string) => request<{ message: string; why: string; pushed: boolean }>(`/commits/${sha}/message`, json('POST', {})),
  reword: (sha: string, message: string) => request<{ commit: string; rewritten: number }>(`/commits/${sha}/reword`, json('POST', { message })),
  markFixed: (id: string) => fetch(`/api/issues/${id}/resolve`, { method: 'POST' }),
  reopenIssue: (id: string) => fetch(`/api/issues/${id}/resolve`, { method: 'DELETE' }),
  graph: () => request<Graph>('/graph'),
  calls: () => request<CallLink[]>('/graph/calls'),
  commits: (limit = 300) => request<CommitItem[]>(`/commits?limit=${limit}`),
  commit: (sha: string) => request<Detail>(`/commits/${sha}`),
  working: () => request<Working>('/working'),
  issues: (state: string) => request<IssueList>(`/issues?state=${state}`),
  review: (sha: string) => request<{ sha: string; queued: boolean }>(`/commits/${sha}/review`, { method: 'POST' }),
  proposeFix: (body: { sha: string; path: string; line: number; title: string; guidance?: string; also?: FixRef[] }) =>
    request<FixProposal & { blocked?: boolean; error?: string }>('/fix', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  checkFix: (id: string) => request<FixCheck>(`/fix/${id}/check`, { method: 'POST' }),
  applyFix: (id: string, message: string) =>
    request<{ commit: string }>(`/fix/${id}/apply`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ message }) }),
  discardFix: (id: string) => fetch(`/api/fix/${id}`, { method: 'DELETE' }),
  ignore: (body: { scope: string; sha: string; path: string; line: number; title: string; reason?: string; team?: boolean }) =>
    request<{ id: string }>('/ignores', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  unignore: async (id: string) => {
    const r = await fetch(`/api/ignores/${id}`, { method: 'DELETE' });
    if (!r.ok) throw new Error((await r.json()).error ?? r.statusText);
  },
  ask: (body: { path: string | null; question: string; node: string | null }) =>
    request<{ answer: string }>('/ask', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  edit: (path: string, instruction: string) =>
    request<FixProposal>('/edit', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path, instruction }) }),
  incidents: () => request<IncidentRow[]>('/incidents'),
  incident: (id: string) => request<{ incident: Incident; markdown: string }>(`/incidents/${id}`),
  createIncident: (title: string, logs: string) => request<{ id: string }>('/incidents', json('POST', { title, logs })),
  updateIncident: (id: string, body: { title?: string; report?: RcaReport; confirmed?: number; runbook?: string }) =>
    request<{ incident: Incident; markdown: string }>(`/incidents/${id}`, json('PUT', body)),
  deleteIncident: (id: string) => fetch(`/api/incidents/${id}`, { method: 'DELETE' }),
  incidentFix: (id: string, cause: number) => request<FixProposal>(`/incidents/${id}/fix`, json('POST', { cause })),
  incidentTest: (id: string, cause: number) => request<FixProposal>(`/incidents/${id}/test`, json('POST', { cause })),
  incidentRule: (id: string, cause: number) => request<{ yaml: string }>(`/incidents/${id}/rule`, json('POST', { cause })),
  incidentRunbook: (id: string) => request<{ markdown: string }>(`/incidents/${id}/runbook`, json('POST', {})),
  saveRunbook: (id: string, markdown: string) => request<FixProposal>(`/incidents/${id}/runbook/save`, json('POST', { markdown })),
  saveRule: (yaml: string, team: boolean) => request<{ saved: number; file: string; total: number }>('/rules/learned', json('POST', { yaml, team })),
  issueTest: (body: { path: string; line: number; title: string; detail: string }) => request<FixProposal>('/tests', json('POST', body)),
  release: (to?: string) =>
    request<{ report: ReleaseReport; markdown: string; refs: { ref: string; label: string }[] }>(
      `/release${to ? `?to=${encodeURIComponent(to)}` : ''}`),
  dependencies: () => request<DependencyReport>('/dependencies'),
  file: (path: string, sha: string | null) =>
    request<{ content: string }>(`/file?path=${encodeURIComponent(path)}${sha ? `&sha=${sha}` : ''}`),
};
