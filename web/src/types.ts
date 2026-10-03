/**
 * 前端 DTO 类型（与 apps/api/src/routes/*.ts 返回结构一一对应）。
 */

export type Mode = "recognition" | "spelling" | "cloze";
export type SessionKind = "learn" | "review" | "test" | "drill" | "sheet";
export type PlanKind = "daily" | "test";
export type PlanStatus = "active" | "paused" | "archived";

export interface Paged<T> {
  items: T[];
  total: number;
  page?: number;
  limit?: number;
}

export interface Accuracy {
  correct: number;
  total: number;
  rate: number | null;
}

// ---------------- 今日 ----------------

export interface TodayPlanCard {
  planId: string;
  name: string;
  kind: PlanKind;
  modes: Mode[];
  source: "self" | "assigned";
  creatorName: string;
  totalWords: number;
  learnedWords: number;
  newPerDay: number;
  newDoneToday: number;
  newLeft: number;
  newAvailable: number;
  reviewPerDay: number;
  reviewDoneToday: number;
  dueCount: number;
  reviewLeft: number;
  testSize: number;
  testResult: { sessionId: string; correct: number; total: number; completedAt: string } | null;
  activeSessions: { id: string; kind: SessionKind; total: number }[];
  doneToday: boolean;
}

export interface TodayData {
  day: string;
  plans: TodayPlanCard[];
  totals: { newLeft: number; reviewLeft: number; newDoneToday: number; reviewDoneToday: number; pendingTests: number };
  drillAvailable: number;
  /** 只给下一份（进行中的优先，否则编号最小的未测单子） */
  sheet: TodaySheet | null;
  streak: number;
  learnedWords: number;
  stats: { newWords: number; reviewedWords: number; answers: number; minutes: number; newLeft: number; reviewLeft: number; pendingTests: number };
}

// ---------------- 单词单 ----------------

export type SheetReason =
  | { kind: "retest" }
  | { kind: "wrong"; count: number }
  | { kind: "lapse"; count: number }
  | { kind: "learning" }
  | { kind: "consolidating" }
  | { kind: "dueSoon" }
  | { kind: "sessionWrong" }
  | { kind: "unlearned" };

/** 选词来源 */
export type SheetSource =
  | { kind: "unfamiliar" }
  | { kind: "session"; sessionId: string }
  | { kind: "unit"; unitId: string }
  | { kind: "book"; bookId: string }
  /** 目标：未测的词 / 要学的词（spec 0003）；bookId 省略时为全部目标词 */
  | { kind: "target"; status: TargetSheetStatus; bookId?: string };

/** 「某次测试的错词」可选的学习组 */
export interface SheetSourceSession {
  id: string;
  kind: SessionKind;
  planName: string;
  dayKey: string;
  startedAt: string;
  status: "active" | "completed";
  wrongCount: number;
}

export interface SheetCandidate {
  wordId: string;
  spelling: string;
  phonetic: string | null;
  partOfSpeech: string | null;
  definition: string;
  score: number;
  reasons: SheetReason[];
}

export interface SheetListItem {
  id: string;
  seq: number;
  wordCount: number;
  createdAt: string;
  creatorName: string;
  status: "pending" | "testing" | "tested";
  firstResult: { sessionId: string; correct: number; total: number } | null;
  activeSessionId: string | null;
}

export interface SheetWord {
  wordId: string;
  spelling: string;
  phonetic: string | null;
  partOfSpeech: string | null;
  definition: string;
}

export interface SheetDetail {
  id: string;
  seq: number;
  createdAt: string;
  student: { id: string; name: string };
  modes: Mode[];
  words: SheetWord[];
}

export interface TodaySheet {
  id: string;
  seq: number;
  wordCount: number;
  activeSessionId: string | null;
  /** 除这一份外还有几份未测 */
  remaining: number;
}

// ---------------- 学习组 ----------------

export interface SessionItem {
  wordId: string;
  spelling: string;
  /** 拼写题标准答案（去掉括号注释） */
  answer: string;
  phonetic: string | null;
  partOfSpeech: string | null;
  definition: string;
  example: string | null;
  exampleCn: string | null;
  type: string;
  isNew: boolean;
  options: string[];
  /** 该词实际可出的题型（缺例句的词没有挖空题） */
  modes?: Mode[];
  /** 挖空题面与中文翻译 */
  cloze?: string | null;
  clozeCn?: string | null;
  /** 进行中检测脱敏后下发：拼写答案字母数与单词数 */
  letters?: number;
  wordsInAnswer?: number;
}

export interface SessionAnswer {
  wordId: string;
  mode: Mode;
  phase: "practice" | "consolidate" | "test";
  attempt: number;
  correct: boolean | null;
  userAnswer: string | null;
  hintUsed: boolean;
  /** 点了「不会」：按答错计，记录里显示「不会」 */
  dontKnow?: boolean;
}

export interface SessionResult {
  words: number;
  settled: number;
  unsettled: number;
  correctFirst: number;
  totalFirst: number;
  accuracy: number | null;
  durationMs: number;
  newLearned: number;
  ratings: Record<string, number>;
  wrongWordIds: string[];
}

export interface StudySessionData {
  id: string;
  userId: string;
  kind: SessionKind;
  status: "active" | "completed";
  planId: string | null;
  sheetId: string | null;
  planName: string | null;
  modes: Mode[];
  items: SessionItem[];
  answers: SessionAnswer[];
  progress: Record<string, unknown> | null;
  result: SessionResult | null;
  startedAt: string;
  completedAt: string | null;
  isOwner: boolean;
}

// ---------------- 计划 ----------------

export interface PlanTargetRef {
  type: "class" | "user";
  id: string;
  name: string;
  email?: string;
}

export interface Plan {
  id: string;
  name: string;
  kind: PlanKind;
  status: PlanStatus;
  newPerDay: number;
  reviewPerDay: number;
  modes: Mode[];
  order: "sequential" | "random";
  testSize: number;
  testScope: "all" | "learned";
  startDate: string | null;
  endDate: string | null;
  creator: { id: string; name: string };
  units: { id: string; name: string; bookId: string; bookName: string }[];
  targets: PlanTargetRef[];
  wordCount: number;
  targetsMe: boolean;
  isSelfPlan: boolean;
  canEdit: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface PlanInput {
  name: string;
  kind: PlanKind;
  status?: PlanStatus;
  newPerDay: number;
  reviewPerDay: number;
  modes: Mode[];
  order: "sequential" | "random";
  testSize: number;
  testScope: "all" | "learned";
  startDate?: string | null;
  endDate?: string | null;
  unitIds: string[];
  targets: { classIds: string[]; userIds: string[] };
}

export interface PlanProgressItem {
  userId: string;
  name: string;
  email: string;
  learnedWords: number;
  totalWords: number;
  today: {
    newDone: number;
    newLeft: number;
    reviewDone: number;
    reviewLeft: number;
    doneToday: boolean;
    testResult: TodayPlanCard["testResult"];
  } | null;
}

// ---------------- 词书 ----------------

export interface Book {
  id: string;
  name: string;
  description: string | null;
  isSystem: boolean;
  ownerName: string | null;
  unitCount: number;
  wordCount: number;
  canEdit: boolean;
  createdAt: string;
  /** 学段（spec 0005）：primary | junior | exam，未设置为 null */
  level?: string | null;
}

export interface BookDetail {
  id: string;
  name: string;
  description: string | null;
  isSystem: boolean;
  ownerName: string | null;
  canEdit: boolean;
  /** 学段（spec 0005）：primary | junior | exam，未设置为 null */
  level?: string | null;
  units: { id: string; name: string; sortOrder: number; wordCount: number }[];
}

export interface UnitWord {
  id: string;
  spelling: string;
  type: string;
  phonetic: string | null;
  partOfSpeech: string | null;
  definition: string;
  example: string | null;
  exampleCn: string | null;
  sortOrder: number;
  usedByUnits: number;
  exampleSource?: string | null;
}

export interface UnitWordsData extends Paged<UnitWord> {
  unit: { id: string; name: string; bookId: string; bookName: string; canEdit: boolean };
}

export interface ImportPreviewEntry {
  spelling: string;
  phonetic: string;
  partOfSpeech: string;
  definition: string;
  type: "word" | "phrase";
  status: "ok" | "warning" | "error";
  issues: string[];
  raw: string;
  line: number;
  existing: boolean;
  existingDefinition: string | null;
}

export interface ImportPreview {
  /** texts：单元里的句型 / 课文段（spec 0004；没有时缺省） */
  units: { name: string; entries: ImportPreviewEntry[]; texts?: PreviewText[] }[];
  stats: { units: number; entries: number; ok: number; warning: number; error: number; existing: number; texts?: number; sentences?: number };
}

// ---------------- 句子与篇（spec 0004） ----------------

export type TextKind = "text" | "list";

/** 句中关联到的词（position 是第几个词，短语取起始位置；form 是句中写法） */
export interface SentenceWordRef {
  wordId: string;
  position: number;
  form: string;
}

export interface Sentence {
  id: string;
  en: string;
  cn: string;
  frame: string | null;
  source: string;
  paragraph: number;
  words: SentenceWordRef[];
}

/** 单元的篇：课文（text）或句型清单（list） */
export interface UnitText {
  id: string;
  unitId: string;
  kind: TextKind;
  title: string;
  titleCn: string | null;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
  sentences: Sentence[];
}

/** 分析结果里的一个词；wordId 为 null 表示尚未入库（导入预览里的新词） */
export interface AnalyzedWord {
  wordId: string | null;
  spelling: string;
  definition: string;
  position: number;
  form: string;
}

export interface SentenceAnalysis {
  tokens: { text: string; index: number }[];
  words: AnalyzedWord[];
  /** 超纲词 */
  outOfScope: AnalyzedWord[];
  /** 词库外的词（人名、地名等） */
  unknown: { text: string; index: number }[];
}

export interface PreviewSentence {
  en: string;
  cn: string;
  /** 句型骨架；没有为空串 */
  frame: string;
  paragraph: number;
  status: "ok" | "warning" | "error";
  issues: string[];
  raw: string;
  line: number;
  words: AnalyzedWord[];
  outOfScope: AnalyzedWord[];
  unknown: string[];
}

export interface PreviewText {
  kind: TextKind;
  title: string;
  titleCn: string;
  line: number;
  sentences: PreviewSentence[];
}

export interface TextsPreview {
  texts: PreviewText[];
  stats: { texts: number; sentences: number; ok: number; warning: number; error: number };
}

/** 新建 / 导入篇的请求体 */
export interface TextInput {
  title: string;
  titleCn?: string | null;
  kind: TextKind;
  sentences: SentenceInput[];
}

export interface SentenceInput {
  id?: string;
  en: string;
  cn: string;
  frame?: string | null;
  paragraph?: number;
}

export interface SentenceFrom {
  type: "example" | "unitText" | "passage";
  wordId?: string;
  spelling?: string;
  textId?: string;
  kind?: TextKind;
  title?: string;
  unitId?: string;
  unitName?: string;
  bookId?: string;
  bookName?: string;
  passageId?: string;
}

export interface WordSentenceItem extends Sentence {
  from: SentenceFrom;
}

/** GET /words/:id/sentences：按来源分组 */
export interface WordSentences {
  examples: WordSentenceItem[];
  patterns: WordSentenceItem[];
  texts: WordSentenceItem[];
  passages: WordSentenceItem[];
}

/** GET /words/:id */
export interface WordDetail {
  id: string;
  spelling: string;
  type: string;
  phonetic: string | null;
  partOfSpeech: string | null;
  definition: string;
}

// ---------------- 班级 ----------------

export interface ClassItem {
  id: string;
  name: string;
  inviteCode: string;
  archived: boolean;
  teacherId: string;
  teacherName: string;
  memberCount: number;
  planCount: number;
  createdAt: string;
}

export interface ClassDetail {
  id: string;
  name: string;
  inviteCode: string;
  archived: boolean;
  teacher: { id: string; name: string };
  members: { id: string; name: string; email: string; joinedAt: string; managedByMe: boolean }[];
  plans: { id: string; name: string; kind: PlanKind; status: PlanStatus; newPerDay: number }[];
}

export type StudentTodayStatus = "done" | "in-progress" | "not-started" | "no-plan";

export interface ClassOverview {
  class: { id: string; name: string; inviteCode: string; memberCount: number };
  day: string;
  summary: { doneToday: number; inProgress: number; notStarted: number; noPlan: number; accuracy7d: Accuracy };
  students: {
    userId: string;
    name: string;
    email: string;
    joinedAt: string;
    streak: number;
    lastActiveDay: string | null;
    learnedWords: number;
    today: {
      status: StudentTodayStatus;
      newDone: number;
      newLeft: number;
      reviewDone: number;
      reviewLeft: number;
      pendingTests: number;
      answers: number;
      minutes: number;
    };
    accuracy7d: Accuracy;
    minutes7d: number;
    activeDays7: number;
    /** 目标覆盖（spec 0003）：按学生自己的有效目标；没有目标为 null */
    coverage: ClassStudentCoverage | null;
  }[];
  hardWords: { wordId: string; spelling: string; definition: string; wrong: number; total: number; rate: number }[];
  activeByDay: { day: string; activeStudents: number; answers: number; accuracy: number | null }[];
}

// ---------------- 目标词书与覆盖进度（spec 0003） ----------------

/** 每个目标词的覆盖状态：未测 / 要学 / 会了 */
export type CoverageStatus = "untested" | "learning" | "known";
/** 单词单 target 来源可选的状态 */
export type TargetSheetStatus = "untested" | "learning";

/** 目标里的一本词书（GET/PUT /classes/:id/target-books、/me/target-books） */
export interface TargetBook {
  id: string;
  name: string;
}

/** GET /me/target-books：source 为 class 时 classes 是所在班级，books 为班级目标的并集 */
export interface MyTargetBooks {
  source: "class" | "own" | "none";
  classes: { id: string; name: string }[];
  books: TargetBook[];
}

export interface CoverageCounts {
  target: number;
  tested: number;
  known: number;
  learning: number;
  untested: number;
}

/** GET /records/coverage */
export interface CoverageData {
  source: MyTargetBooks["source"];
  total: CoverageCounts;
  books: (CoverageCounts & { bookId: string; name: string })[];
}

/** GET /records/coverage/words 的一项 */
export interface CoverageWord {
  wordId: string;
  spelling: string;
  phonetic: string | null;
  partOfSpeech: string | null;
  definition: string;
  status: CoverageStatus;
}

/** 班级概览里学生的覆盖数字 */
export interface ClassStudentCoverage {
  target: number;
  tested: number;
  learning: number;
}

export interface MyClass {
  id: string;
  name: string;
  teacherName: string;
  archived: boolean;
  joinedAt: string;
}

// ---------------- 记录 ----------------

export interface RecordsSummary {
  day: string;
  streak: number;
  activeDays30: number;
  lastActiveDay: string | null;
  learnedWords: number;
  mastery: { learning: number; consolidating: number; mastered: number };
  dueToday: number;
  today: { newWords: number; reviewedWords: number; answers: number; minutes: number; newLeft: number; reviewLeft: number; pendingTests: number };
  accuracy7d: Accuracy;
  accuracy30d: Accuracy;
  minutes7d: number;
}

export interface DailyPoint {
  day: string;
  newWords: number;
  reviewedWords: number;
  answers: number;
  correct: number;
  firstAttempts: number;
  minutes: number;
}

export interface SessionListItem {
  id: string;
  kind: SessionKind;
  status: "active" | "completed";
  planId: string | null;
  planName: string | null;
  words: number;
  answers: number;
  result: SessionResult | null;
  dayKey: string;
  startedAt: string;
  completedAt: string | null;
}

export interface SessionRecordDetail {
  id: string;
  user: { id: string; name: string };
  kind: SessionKind;
  status: "active" | "completed";
  planId: string | null;
  planName: string | null;
  modes: Mode[];
  result: SessionResult | null;
  startedAt: string;
  completedAt: string | null;
  words: {
    wordId: string;
    spelling: string;
    definition: string;
    phonetic: string | null;
    partOfSpeech: string | null;
    answers: { mode: Mode; phase: string; attempt: number; correct: boolean; userAnswer: string | null; hintUsed: boolean; dontKnow: boolean; durationMs: number }[];
    review: { rating: number; dueAfter: string; stabilityAfter: number } | null;
  }[];
}

export type MasteryLevel = "learning" | "consolidating" | "mastered";
export type WordFilter = "all" | "due" | "difficult" | "mastered" | "consolidating" | "learning";

export interface MemoryWord {
  wordId: string;
  spelling: string;
  phonetic: string | null;
  partOfSpeech: string | null;
  definition: string;
  due: string;
  isDue: boolean;
  stability: number;
  difficulty: number;
  level: MasteryLevel;
  levelLabel: string;
  reps: number;
  lapses: number;
  lastReview: string | null;
  introducedDay: string;
}

export interface WordHistory {
  word: { id: string; spelling: string; phonetic: string | null; partOfSpeech: string | null; definition: string; example: string | null; exampleCn: string | null };
  memory: {
    due: string;
    stability: number;
    difficulty: number;
    reps: number;
    lapses: number;
    state: number;
    lastReview: string | null;
    introducedDay: string;
    level: MasteryLevel;
    levelLabel: string;
  } | null;
  reviewLogs: { id: string; rating: number; stateBefore: number; stabilityAfter: number; difficultyAfter: number; dueAfter: string; reviewedAt: string; dayKey: string; sessionId: string | null }[];
  answers: { id: string; sessionId: string; mode: Mode; phase: string; attempt: number; correct: boolean; userAnswer: string | null; hintUsed: boolean; dontKnow: boolean; dayKey: string; createdAt: string }[];
}

// ---------------- AI ----------------

export interface AiStatus {
  enabled: boolean;
  provider: "openai" | "anthropic" | "none";
  model: string | null;
  baseUrl: string | null;
  audio: boolean;
}

export interface PassageQuestion {
  q: string;
  a: string;
}

export interface PassageDetail {
  id: string;
  title: string;
  titleCn: string | null;
  passage: string;
  passageCn: string | null;
  questions: PassageQuestion[];
  words: { id: string; spelling: string; definition: string; phonetic: string | null }[];
  model: string | null;
  createdAt: string;
  /** spec 0004：逐句结构；没有拆分的旧短文为空数组（更老的后端缺省） */
  sentences?: Sentence[];
}

export interface PassageListItem {
  id: string;
  title: string;
  titleCn: string | null;
  wordCount: number;
  createdAt: string;
}

// ---------------- 文案 ----------------

export const KIND_LABEL: Record<SessionKind, string> = { learn: "新学", review: "复习", test: "检测", drill: "错词强化", sheet: "单词单" };
export const MODE_LABEL: Record<Mode, string> = { recognition: "认义", spelling: "拼写", cloze: "挖空填词" };
export const PLAN_STATUS_LABEL: Record<PlanStatus, string> = { active: "进行中", paused: "已暂停", archived: "已归档" };
export const RATING_LABEL: Record<number, string> = { 1: "忘记", 2: "模糊", 3: "记得", 4: "熟练" };
