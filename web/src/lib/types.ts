export type Severity = 'critical' | 'high' | 'medium' | 'low' | 'info';

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

export interface Member {
  name: string;
  kind: 'method' | 'constructor' | 'field';
  signature: string;
  startLine: number;
  endLine: number;
  value?: string;
}

export interface GraphNode {
  id: string;
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
  kind: 'extends' | 'implements' | 'injects' | 'uses';
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

export type Mode = 'review' | 'incidents' | 'release';

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

export interface CheckItem { id: string; text: string; done: boolean; by?: string; at?: number; custom?: boolean; current: boolean }

export interface SignOff { to: string; items: CheckItem[]; done: number; total: number }
