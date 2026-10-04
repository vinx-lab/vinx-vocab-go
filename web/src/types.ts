/**
 * 前端 DTO 类型（与 apps/api/src/routes/*.ts 返回结构一一对应）。
 */

export type Mode = "recognition" | "spelling" | "cloze";
/** 作答记录里的题型：在线测试的三种，加上默写单批改（spec 0006） */
export type AnswerMode = Mode | "dictation";
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
  /** 今天批改提交的默写单（spec 0006，今日页显示「已批改」） */
  gradedSheets?: TodayGradedSheet[];
  streak: number;
  learnedWords: number;
  stats: { newWords: number; reviewedWords: number; answers: number; minutes: number; newLeft: number; reviewLeft: number; pendingTests: number };
  /** 今日计划里有单元所在的书不在我的目标词书内的计划（spec 0008，卡片标「不在目标词书内」） */
  outsideTargetPlanIds?: string[];
  /** 因班级未开放自主安排而暂停的自建计划数（spec 0008） */
  pausedSelfPlans?: number;
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
  /** 默写单预览才有：单词 / 短语（spec 0006） */
  type?: "word" | "phrase";
}

// ---------------- 默写单（spec 0006） ----------------

/** 单词单格式：对折自测表 / 默写单 */
export type SheetFormat = "selftest" | "dictation";
/** 默写单题型 */
export type DictItemType = "word" | "phrase" | "sentence" | "frame" | "transform";
/** 句子在默写里的状态：未测 / 要学 / 会了 */
export type SentenceDictStatus = "untested" | "learning" | "known";

/** 默写单的句子来源 */
export type SentenceSource =
  | { kind: "text"; textId: string }
  | { kind: "learning" }
  | { kind: "passage"; passageId: string }
  | { kind: "session"; sessionId: string };

/** GET /sheets/sources?unitId= 里单元的一篇 */
export interface SheetSourceText {
  id: string;
  /** list 句型清单（含仿写）| text 课文 */
  kind: "list" | "text";
  title: string;
  titleCn: string | null;
  sentenceCount: number;
  variantCount: number;
}

/** GET /sheets/sources */
export interface SheetSourcesData {
  sessions: SheetSourceSession[];
  learningSentences?: number;
  texts?: SheetSourceText[];
}

/** 默写单预览里的一个句子 */
export interface DictSentenceCandidate {
  sentenceId: string;
  type: "sentence" | "frame" | "transform";
  en: string;
  cn: string;
  frame: string | null;
  prompt: string;
  answer: string;
  status: SentenceDictStatus;
}

/** POST /sheets/preview 的返回（默写单才有 sentences） */
export interface SheetPreviewData {
  items: SheetCandidate[];
  sentences?: DictSentenceCandidate[];
}

/** 默写单明细里的一道题；index 是在 items 里的下标（批改按它提交） */
export interface DictItemView {
  index: number;
  type: DictItemType;
  /** 1 单词、2 短语、3 句子、4 仿写与转换 */
  section: number;
  wordId?: string;
  sentenceId?: string;
  prompt: string;
  answer: string;
  cn?: string;
  origin?: { id: string; en: string; cn: string };
}

/** 默写单的批改结果（成绩单） */
export interface SheetGrading {
  sessionId: string;
  gradedAt: string | null;
  gradedBy: { id: string; name: string };
  selfGraded: boolean;
  correct: number;
  total: number;
  results: { index: number; correct: boolean; userAnswer: string | null }[];
}

export interface TodayGradedSheet {
  id: string;
  seq: number;
  itemCount: number;
  sessionId: string;
  correct: number;
  total: number;
  selfGraded: boolean;
  gradedAt: string;
}

export interface SheetListItem {
  id: string;
  seq: number;
  wordCount: number;
  createdAt: string;
  creatorName: string;
  /** 默写单：pending 待批改 / tested 已批改 */
  status: "pending" | "testing" | "tested";
  firstResult: { sessionId: string; correct: number; total: number } | null;
  activeSessionId: string | null;
  /** spec 0006：格式（旧数据缺省按自测表）、题数、默写单是否自批 */
  format?: SheetFormat;
  itemCount?: number;
  selfGraded?: boolean;
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
  /** spec 0006：格式；默写单的题目（自测表为 []）；批改结果（未批改或自测表为 null） */
  format?: SheetFormat;
  items?: DictItemView[];
  grading?: SheetGrading | null;
}

export interface TodaySheet {
  id: string;
  seq: number;
  wordCount: number;
  activeSessionId: string | null;
  /** 除这一份外还有几份未测 */
  remaining: number;
  /** spec 0006：默写单显示「待批改」；itemCount 为题数 */
  format?: SheetFormat;
  itemCount?: number;
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
  /** 默写单的句子题成绩（spec 0006；其他组没有） */
  sentences?: { total: number; correct: number; wrongSentenceIds: string[] };
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
  /** 学生自建的计划，因所在班级未开放自主安排而暂停（算出来的，status 不变；spec 0008） */
  selfPlanPaused?: boolean;
  /** 安排给我的计划有单元所在的书不在我的目标词书内（spec 0008） */
  outsideTarget?: boolean;
  createdAt: string;
  updatedAt: string;
}

/** POST /plans/allowed-books：建计划页的单元选择范围（spec 0008） */
export interface AllowedPlanBooks {
  /** 为假时安排对象都没有目标词书，不约束 */
  constrained: boolean;
  books: TargetBook[];
  /** 安排对象只有自己时，能否自建 */
  selfPlanAllowed: boolean;
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
  /** 是否允许学生自主安排计划（spec 0008） */
  allowSelfPlan: boolean;
}

export interface ClassDetail {
  id: string;
  name: string;
  inviteCode: string;
  archived: boolean;
  teacher: { id: string; name: string };
  members: { id: string; name: string; email: string; joinedAt: string; managedByMe: boolean }[];
  plans: { id: string; name: string; kind: PlanKind; status: PlanStatus; newPerDay: number }[];
  /** 是否允许学生自主安排计划（spec 0008） */
  allowSelfPlan: boolean;
}

/** GET /classes/:id/self-plan-impact：关闭自主安排前的影响（spec 0008） */
export interface SelfPlanImpact {
  students: number;
  selfPlans: number;
  ownBooks: number;
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
    /** 按学生自己的目标算时，目标里含自己追加的书（spec 0008） */
    coverageIncludesOwn?: boolean;
  }[];
  hardWords: { wordId: string; spelling: string; definition: string; wrong: number; total: number; rate: number }[];
  activeByDay: { day: string; activeStudents: number; answers: number; accuracy: number | null }[];
  /** 目标覆盖的口径（spec 0008）：class = 只按本班目标；student = 按每个学生自己的目标（含自选） */
  coverageMode?: "class" | "student";
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
  /** 只在 /me/target-books 的 books 里：class 由班级设置（锁定），own 自己追加（spec 0008） */
  source?: "class" | "own";
  /** source 为 class 时，设置这本书的班级 */
  classNames?: string[];
}

/**
 * GET /me/target-books：source 为 class 时 classes 是所在班级，books 为班级目标的并集，
 * 能追加时（canEditOwn）再并上自己追加的（spec 0008）
 */
export interface MyTargetBooks {
  source: "class" | "own" | "none";
  classes: { id: string; name: string }[];
  books: TargetBook[];
  /** 自己设置的全部目标（含与班级重复、当前不生效的）；编辑时整体提交它 */
  ownBooks: TargetBook[];
  /** 能否追加自己的目标，同时也是能否自建计划 */
  canEditOwn: boolean;
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
  /** spec 0006：已测词里最近一次正式测试是自批默写单的词数；比例 = selfGraded / tested，没有已测为 null */
  selfGraded?: number;
  selfGradedRatio?: number | null;
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
  /** 默写单批改组才有（spec 0006） */
  format?: SheetFormat;
  selfGraded?: boolean;
}

/** 默写单批改组里的一道句子题 */
export interface SessionDetailSentence {
  index: number;
  sentenceId: string;
  type: DictItemType;
  en: string;
  cn: string;
  prompt: string;
  correct: boolean;
  userAnswer: string | null;
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
    answers: { mode: AnswerMode; phase: string; attempt: number; correct: boolean; userAnswer: string | null; hintUsed: boolean; dontKnow: boolean; durationMs: number }[];
    review: { rating: number; dueAfter: string; stabilityAfter: number } | null;
  }[];
  /** 默写单批改组才有（spec 0006）：格式、批改人、自批标记、句子题的对错 */
  format?: SheetFormat;
  gradedBy?: { id: string; name: string };
  selfGraded?: boolean;
  sentences?: SessionDetailSentence[];
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
  answers: { id: string; sessionId: string; mode: AnswerMode; phase: string; attempt: number; correct: boolean; userAnswer: string | null; hintUsed: boolean; dontKnow: boolean; dayKey: string; createdAt: string }[];
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
export const ANSWER_MODE_LABEL: Record<AnswerMode, string> = { ...MODE_LABEL, dictation: "默写" };
export const PLAN_STATUS_LABEL: Record<PlanStatus, string> = { active: "进行中", paused: "已暂停", archived: "已归档" };
export const RATING_LABEL: Record<number, string> = { 1: "忘记", 2: "模糊", 3: "记得", 4: "熟练" };
