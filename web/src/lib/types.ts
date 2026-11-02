export type Severity = 'critical' | 'high' | 'medium' | 'low' | 'info';

/** Another place of an issue, in the review that reported it. */
export interface FixRef { sha: string; path: string; line: number; title: string }

export interface FixProposal {
  id: string;
  sha: string;
  path: string;
  paths: string[];
  finding: Finding;
  patch: string;
  message: string;
  branch: string;
  /** Fixes uncommitted changes: applied to the working tree, not committed. */
  working?: boolean;
}

/** Instant rule checks on the uncommitted changes in the working tree. */
export interface Working {
  files: FileReview[];
  changes: { path: string; newPath?: string; changeType: string; addedLines: number[] }[];
  findings: number;
}

export interface Status {
  repo: string;
  path: string;
  branch: string;
  viewing?: boolean;
  checkedOut?: string;
  tracking: string;
  head: string;
  model: string;
  llmProblem: string;
  graphSource: string;
  graphBuiltAt: number;
  canFix: boolean;
  user: string;
  /** merge, rebase, cherry-pick or revert in progress in the working tree. */
  operation?: string;
  conflicts?: string[];
  rules: number;
  models: { review: string; summary: string; fix: string; ask: string };
}

export interface MethodCost { complexity: number; loopDepth: number; dbCalls: number; remoteCalls: number; loopIo: number; params: number; lines: number; estimate: string }

export interface Member {
  cost?: MethodCost;
  name: string;
  kind: 'method' | 'constructor' | 'field';
  signature: string;
  startLine: number;
  endLine: number;
  value?: string;
}

export interface GraphNode {
  id: string;
  fqn: string;
  module?: string;
  feignTarget?: string;
  name: string;
  pkg: string;
  kind: string;
  stereotype: string;
  file: string;
  startLine: number;
  endLine: number;
  annotations: string[];
  members: Member[];
  endpoints: string[];
}

export interface GraphEdge {
  source: string;
  target: string;
  kind: 'extends' | 'implements' | 'injects' | 'uses' | 'feign';
  via?: string[];
}

export interface Graph {
  nodes: GraphNode[];
  edges: GraphEdge[];
  source: string;
  builtAt: number;
  parseErrors: number;
}

export interface Commit {
  sha: string;
  shortSha: string;
  message: string;
  fullMessage: string;
  author: string;
  time: number;
  parents: string[];
}

export interface CommitItem {
  commit: Commit;
  status?: ReviewStatus;
  risk?: string;
  counts?: Partial<Record<Severity, number>>;
  fixed?: number;
  messageProblem?: boolean;
}

export interface FileChange {
  changeType: 'ADD' | 'MODIFY' | 'DELETE' | 'RENAME' | 'COPY';
  oldPath?: string;
  newPath?: string;
  path: string;
  addedLines: number[];
  deletedAt: number[];
  patch: string;
  binary?: boolean;
}

export interface ChangedType {
  id: string;
  file: string;
  status: 'added' | 'modified' | 'deleted';
  members: string[];
}

export interface Analysis {
  commit: Commit;
  files: FileChange[];
  changed: ChangedType[];
  impacted: string[];
}

export type ReviewStatus = 'queued' | 'running' | 'done' | 'failed';

export interface Finding {
  line: number;
  endLine: number;
  severity: Severity;
  category: string;
  title: string;
  detail: string;
  suggestion: string;
  nodeId: string;
  onChangedLine: boolean;
  source: 'rule' | 'llm';
  ref?: string;
  snippet?: string;
  ignored?: boolean;
  ignoreId?: string;
  group?: string;
  groupRoot?: boolean;
  firstSeen?: string;
  state?: 'open' | 'resolved' | 'ignored';
  fixedIn?: string;
  fixedBy?: string;
  fixedAt?: number;
  fixedMsg?: string;
  uncommitted?: boolean;
}

export interface Issue {
  kind?: string;
  urgency?: string;
  change?: string;
  id: string;
  state: 'open' | 'resolved' | 'ignored';
  severity: Severity;
  category: string;
  title: string;
  detail: string;
  suggestion: string;
  ref?: string;
  source: 'rule' | 'llm';
  path: string;
  line: number;
  snippet: string;
  firstSeen: string;
  firstTime: number;
  firstBy: string;
  firstMsg: string;
  lastSeen: string;
  fixedIn?: string;
  fixedBy?: string;
  fixedAt?: number;
  fixedMsg?: string;
  uncommitted?: boolean;
  manual?: boolean;
  wasFixedIn?: string;
  reopenedIn?: string;
  reopenedBy?: string;
  reopenedAt?: number;
  reopenedMsg?: string;
  commits: string[];
  ignoreId?: string;
  group?: string;
  groupRoot?: boolean;
  reviewLine: number;
}

export interface IssueList {
  issues: Issue[];
  counts: Partial<Record<'open' | 'resolved' | 'ignored', number>>;
  openBySeverity: Partial<Record<Severity, number>>;
}

export interface FileReview {
  path: string;
  summary: string;
  findings: Finding[];
  error?: string;
  pending?: boolean;
  skipped?: string;
}

export interface Review {
  sha: string;
  status: ReviewStatus;
  model: string;
  startedAt: number;
  finishedAt: number;
  summary: string;
  risk: 'low' | 'medium' | 'high' | '';
  highlights: string[];
  files: FileReview[];
  error?: string;
  messageProblem?: string;
  suggestedMessage?: string;
  analysers?: string[];
  analysisProblems?: string[];
}

export interface Detail {
  analysis: Analysis;
  review: Review | null;
  /** path|line|title -> "instant" (mechanical) or "ready" (prepared by the model). */
  fixes: Record<string, 'instant' | 'ready'>;
  /** Earlier issues this commit fixed. */
  resolved: Issue[];
}

export interface OpenFile {
  key: string;
  path: string;
  sha: string | null;
  content: string;
}

export type FindingWithPath = Finding & { path: string };

export type Tab = 'issues' | 'review' | 'code' | 'diff' | 'type' | 'ask';

export type IgnoreScope = 'commit' | 'code' | 'rule';

export interface AskMessage {
  role: 'user' | 'assistant';
  text: string;
  path: string | null;
  error?: boolean;
}

export type Mode = 'review' | 'repo' | 'branches' | 'mine' | 'team' | 'release' | 'incidents' | 'activity' | 'memory';

export interface Frame { class: string; method: string; file: string; line: number }

export interface ErrorEvent {
  exception: string;
  message: string;
  logger?: string;
  level?: string;
  frames: Frame[];
  causedBy?: string[];
  count: number;
  firstTime?: string;
  lastTime?: string;
}

export interface BlameLine { sha: string; author: string; time: number; summary: string }

export interface IncidentLocation {
  path: string;
  line: number;
  class: string;
  method: string;
  nodeId: string;
  snippet: string;
  origin: boolean;
  hits: number;
  blame: BlameLine;
  issues?: string[];
}

export interface Suspect { sha: string; author: string; time: number; message: string; score: number; reasons: string[] }

export interface Evidence { kind: 'log' | 'code' | 'commit' | 'issue' | string; text: string; path?: string; line?: number; sha?: string }

export interface Cause {
  title: string;
  explanation: string;
  confidence: 'high' | 'medium' | 'low';
  evidence: Evidence[];
  fix: string;
  path?: string;
  line?: number;
  sha?: string;
}

export interface RcaReport { summary: string; timeline: string; impact: string; rootCause: string; fix: string; prevention: string }

export interface SimilarIncident { id: string; title: string; score: number; rootCause: string; createdAt: number }

export interface Incident {
  id: string;
  title: string;
  createdAt: number;
  status: 'analysing' | 'done' | 'failed' | 'confirmed';
  error?: string;
  logBytes: number;
  excerpt: string;
  errors: ErrorEvent[];
  locations: IncidentLocation[];
  suspects: Suspect[];
  similar: SimilarIncident[];
  causes: Cause[];
  timeline: string[];
  report: RcaReport;
  confirmed: number;
  runbook?: string;
  model: string;
}

export interface IncidentRow {
  id: string;
  title: string;
  status: Incident['status'];
  createdAt: number;
  errors: number;
  occurrences: number;
  confirmed: boolean;
  topCause: string;
}


export interface ReleaseReport {
  to: string;
  toLabel: string;
  files: number;
  lines: number;
  reviewed: number;
  sources: number;
  notReviewed: string[];
  modules: { name: string; files: number; issues: number }[];
  open: Issue[];
  ignored: number;
  buildFindings: Finding[];
  risk: 'low' | 'medium' | 'high';
  reasons: string[];
  checklist: string[];
}

export interface DepRow { group: string; artifact: string; version: string; scope: string; managed: boolean; status: 'ok' | 'warn' | 'bad'; note: string }

export interface DependencyReport { tool: string; java: string; springBoot: string; support: string; deps: DepRow[]; findings: Finding[]; file: string }



export interface RepoReport {
  name: string; head: string; branch: string; files: number; javaFiles: number; testFiles: number; lines: number; types: number; methods: number; commits: number;
  contributors: { name: string; commits: number }[];
  languages: Record<string, number>; roles: Record<string, number>;
  services: { name: string; port?: string; kind: string; files: number; lines: number; types: number; tests: number; endpoints: number; calls: string[]; calledBy: string[]; usesLibs: string[]; deps: string[]; open: Partial<Record<Severity, number>>; commits: number }[];
  endpoints: { service: string; route: string; handler: string; class: string; node: string; callers: number }[];
  calls: { from: string; to: string; client: string; node: string; routes: string[] }[];
  hotspots: { path: string; commits: number; open: number; lines: number }[];
  costly: { class: string; node: string; method: string; file: string; line: number; cost: MethodCost }[];
  largest: { Name: string; Node: string; File: string; Lines: number; Members: number }[];
  constants: { class: string; name: string; value: string; file: string; line: number }[];
  issues: Record<string, number>; urgency: Record<string, number>; severity: Record<string, number>;
}

export interface BranchReport {
  base: string; tookMs: number; local: string[];
  branches: { name: string; remote: boolean; sha: string; ahead: number; behind: number; authors: string[]; lastBy: string; lastAt: number; lastMsg: string; files: string[]; conflicts: string[]; merged: boolean }[];
  alerts: { severity: 'conflict' | 'overlap'; title: string; detail: string; a: string; b: string; people: string[]; files: string[] }[];
}

export interface Activity {
  id: string; at: number; kind: string; title: string; status: 'running' | 'done' | 'failed' | 'cancelled'; model?: string; sha?: string; path?: string; incident?: string;
  knowledge?: string[]; note?: string; error?: string; tookMs: number; promptTokens?: number; replyTokens?: number; system?: string; user?: string; response?: string;
}

export interface MemoryItem { id: string; at: number; by: string; kind: string; text: string; title?: string; ref?: string; path?: string; snippet?: string; team: boolean }

export interface MemoryReport {
  items: MemoryItem[];
  lessons: { id: string; title: string; severity: string; body: string; source: string }[];
  rules: { id: string; title: string; source: string }[];
  ignores: { id: string; scope: string; title?: string; key?: string; reason?: string; team?: boolean; path?: string; snippet?: string }[];
  builtinEntries: number; builtinRules: number;
}

export interface PlannedCommit { message: string; why: string; files: { path: string; change: string; added: number; removed: number; issues: number }[] }

export interface BranchRef { name: string; remote: boolean; sha: string; message: string; by: string; at: number; checkedOut: boolean }

export interface HistoryCommit { sha: string; parents: string[]; author: string; at: number; subject: string; refs: string[]; status: string; issues: number }

export interface Flags { menus: Record<string, boolean>; panels: Record<string, boolean>; features: Record<string, boolean>; problems?: string[]; sources: string[] }
export interface PersonStats { name: string; commits: number; added: number; removed: number; files: number; owned: number }
export interface PeopleReport { head: string; people: PersonStats[]; totalLines: number; tookMs: number }


export interface DevPoint { title: string; detail: string; advice?: string; issues?: string[] }
export interface DevShare { name: string; lines: number; commits: number }
export interface DevProfile {
  name: string; commits: number; reviewed: number; added: number; removed: number; owned: number; files: number; testShare: number; medianCommit: number; bigCommits: number; badMessages: number;
  skills: DevShare[]; areas: DevShare[]; introduced: number; openNow: number; serious: number; severity: { name: string; n: number }[]; kinds: { name: string; n: number }[]; rate: number;
  fixed: number; fixedOwn: number; fixedOthers: number; avgFixDays: number; enoughEvidence: boolean; strengths: DevPoint[]; improve: DevPoint[];
}
export interface TeamReport { people: DevProfile[]; teamRate: number }

export interface ImpactPlace { class: string; method: string; file: string; line: number; depth: number; confirmed: boolean }
export interface ChangedMethod { class: string; method: string; file: string; line: number; status: 'added' | 'modified' | 'signature' | 'removed'; callers: number; tests: number }
export interface ImpactReport { sha: string; changed: ChangedMethod[]; callers: ImpactPlace[]; callerCount: number; possible: number; routes: (ImpactPlace & { route: string })[]; tests: ImpactPlace[]; classes: number; untestedRisk: number }
