-- Vinx Vocab 初始结构：逐张对应旧 Prisma schema 的 17 张表（表名、列名、字符串 id 不变）。
-- 类型约定：id / 字符串 / 枚举 → TEXT；Int → INTEGER；Float → REAL；Boolean → INTEGER(0/1)；
-- DateTime → TEXT（UTC 毫秒 RFC3339，如 2026-09-28T05:51:26.656Z）；Json 与 String[] → TEXT(JSON)。
-- 时间默认值与 Prisma 的 @default(now()) 对应；@updatedAt 由应用写入（默认值只是兜底）。

CREATE TABLE "User" (
  "id"           TEXT NOT NULL PRIMARY KEY,
  "email"        TEXT NOT NULL,
  "passwordHash" TEXT NOT NULL,
  "name"         TEXT NOT NULL,
  "role"         TEXT NOT NULL DEFAULT 'student',
  "currentGrade" TEXT,
  "theme"        TEXT NOT NULL DEFAULT 'system',
  "createdById"  TEXT,
  "createdAt"    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "updatedAt"    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE UNIQUE INDEX "User_email_key" ON "User"("email");

CREATE TABLE "Book" (
  "id"          TEXT NOT NULL PRIMARY KEY,
  "name"        TEXT NOT NULL,
  "description" TEXT,
  "isSystem"    INTEGER NOT NULL DEFAULT 0,
  "ownerId"     TEXT REFERENCES "User"("id") ON DELETE SET NULL ON UPDATE CASCADE,
  "sortOrder"   INTEGER NOT NULL DEFAULT 0,
  "createdAt"   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "updatedAt"   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX "Book_ownerId_idx" ON "Book"("ownerId");

CREATE TABLE "Unit" (
  "id"        TEXT NOT NULL PRIMARY KEY,
  "bookId"    TEXT NOT NULL REFERENCES "Book"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "name"      TEXT NOT NULL,
  "sortOrder" INTEGER NOT NULL DEFAULT 0,
  "createdAt" TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE UNIQUE INDEX "Unit_bookId_name_key" ON "Unit"("bookId", "name");
CREATE INDEX "Unit_bookId_sortOrder_idx" ON "Unit"("bookId", "sortOrder");

CREATE TABLE "Word" (
  "id"            TEXT NOT NULL PRIMARY KEY,
  "spelling"      TEXT NOT NULL,
  "type"          TEXT NOT NULL DEFAULT 'word',
  "phonetic"      TEXT,
  "partOfSpeech"  TEXT,
  "definition"    TEXT NOT NULL,
  "example"       TEXT,
  "exampleCn"     TEXT,
  "exampleSource" TEXT,
  "exampleAt"     TEXT,
  "audioFile"     TEXT,
  "createdAt"     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "updatedAt"     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE UNIQUE INDEX "Word_spelling_key" ON "Word"("spelling");

CREATE TABLE "UnitWord" (
  "unitId"    TEXT NOT NULL REFERENCES "Unit"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "wordId"    TEXT NOT NULL REFERENCES "Word"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "sortOrder" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY ("unitId", "wordId")
);
CREATE INDEX "UnitWord_wordId_idx" ON "UnitWord"("wordId");
CREATE INDEX "UnitWord_unitId_sortOrder_idx" ON "UnitWord"("unitId", "sortOrder");

CREATE TABLE "Classroom" (
  "id"         TEXT NOT NULL PRIMARY KEY,
  "name"       TEXT NOT NULL,
  "teacherId"  TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "inviteCode" TEXT NOT NULL,
  "archived"   INTEGER NOT NULL DEFAULT 0,
  "createdAt"  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "updatedAt"  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE UNIQUE INDEX "Classroom_inviteCode_key" ON "Classroom"("inviteCode");
CREATE INDEX "Classroom_teacherId_idx" ON "Classroom"("teacherId");

CREATE TABLE "ClassMember" (
  "classId"  TEXT NOT NULL REFERENCES "Classroom"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "userId"   TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "joinedAt" TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY ("classId", "userId")
);
CREATE INDEX "ClassMember_userId_idx" ON "ClassMember"("userId");

CREATE TABLE "Plan" (
  "id"           TEXT NOT NULL PRIMARY KEY,
  "name"         TEXT NOT NULL,
  "creatorId"    TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "kind"         TEXT NOT NULL DEFAULT 'daily',
  "status"       TEXT NOT NULL DEFAULT 'active',
  "newPerDay"    INTEGER NOT NULL DEFAULT 10,
  "reviewPerDay" INTEGER NOT NULL DEFAULT 50,
  "modes"        TEXT NOT NULL DEFAULT '["recognition","spelling"]',
  "order"        TEXT NOT NULL DEFAULT 'sequential',
  "testSize"     INTEGER NOT NULL DEFAULT 20,
  "testScope"    TEXT NOT NULL DEFAULT 'all',
  "startDate"    TEXT,
  "endDate"      TEXT,
  "createdAt"    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "updatedAt"    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX "Plan_creatorId_idx" ON "Plan"("creatorId");

CREATE TABLE "PlanUnit" (
  "planId"    TEXT NOT NULL REFERENCES "Plan"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "unitId"    TEXT NOT NULL REFERENCES "Unit"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "sortOrder" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY ("planId", "unitId")
);
CREATE INDEX "PlanUnit_unitId_idx" ON "PlanUnit"("unitId");

CREATE TABLE "PlanTarget" (
  "id"      TEXT NOT NULL PRIMARY KEY,
  "planId"  TEXT NOT NULL REFERENCES "Plan"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "classId" TEXT REFERENCES "Classroom"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "userId"  TEXT REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE UNIQUE INDEX "PlanTarget_planId_classId_key" ON "PlanTarget"("planId", "classId");
CREATE UNIQUE INDEX "PlanTarget_planId_userId_key" ON "PlanTarget"("planId", "userId");
CREATE INDEX "PlanTarget_classId_idx" ON "PlanTarget"("classId");
CREATE INDEX "PlanTarget_userId_idx" ON "PlanTarget"("userId");

CREATE TABLE "MemoryState" (
  "id"               TEXT NOT NULL PRIMARY KEY,
  "userId"           TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "wordId"           TEXT NOT NULL REFERENCES "Word"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "due"              TEXT NOT NULL,
  "stability"        REAL NOT NULL,
  "difficulty"       REAL NOT NULL,
  "elapsedDays"      INTEGER NOT NULL DEFAULT 0,
  "scheduledDays"    INTEGER NOT NULL DEFAULT 0,
  "learningSteps"    INTEGER NOT NULL DEFAULT 0,
  "reps"             INTEGER NOT NULL DEFAULT 0,
  "lapses"           INTEGER NOT NULL DEFAULT 0,
  "state"            INTEGER NOT NULL DEFAULT 0,
  "lastReview"       TEXT,
  "introducedAt"     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "introducedPlanId" TEXT,
  "introducedDay"    TEXT NOT NULL,
  "updatedAt"        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE UNIQUE INDEX "MemoryState_userId_wordId_key" ON "MemoryState"("userId", "wordId");
CREATE INDEX "MemoryState_userId_due_idx" ON "MemoryState"("userId", "due");
CREATE INDEX "MemoryState_userId_introducedPlanId_introducedDay_idx" ON "MemoryState"("userId", "introducedPlanId", "introducedDay");

CREATE TABLE "WordSheet" (
  "id"        TEXT NOT NULL PRIMARY KEY,
  "userId"    TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "creatorId" TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "seq"       INTEGER NOT NULL,
  "wordIds"   TEXT NOT NULL DEFAULT '[]',
  "modes"     TEXT NOT NULL DEFAULT '["recognition","spelling"]',
  "createdAt" TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE UNIQUE INDEX "WordSheet_userId_seq_key" ON "WordSheet"("userId", "seq");
CREATE INDEX "WordSheet_creatorId_idx" ON "WordSheet"("creatorId");

CREATE TABLE "StudySession" (
  "id"           TEXT NOT NULL PRIMARY KEY,
  "userId"       TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "planId"       TEXT REFERENCES "Plan"("id") ON DELETE SET NULL ON UPDATE CASCADE,
  "sheetId"      TEXT REFERENCES "WordSheet"("id") ON DELETE SET NULL ON UPDATE CASCADE,
  "kind"         TEXT NOT NULL,
  "wordCount"    INTEGER NOT NULL DEFAULT 0,
  "status"       TEXT NOT NULL DEFAULT 'active',
  "dayKey"       TEXT NOT NULL,
  "snapshot"     TEXT NOT NULL,
  "progress"     TEXT,
  "result"       TEXT,
  "startedAt"    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "completedAt"  TEXT,
  "completedDay" TEXT
);
CREATE INDEX "StudySession_userId_status_idx" ON "StudySession"("userId", "status");
CREATE INDEX "StudySession_userId_completedDay_idx" ON "StudySession"("userId", "completedDay");
CREATE INDEX "StudySession_planId_idx" ON "StudySession"("planId");
CREATE INDEX "StudySession_sheetId_idx" ON "StudySession"("sheetId");
-- 手工约束（旧库 K12）：同一学生、同一计划（无计划记为 ''）、同一类型最多一个进行中的学习组
CREATE UNIQUE INDEX "StudySession_active_uniq" ON "StudySession" ("userId", COALESCE("planId", ''), "kind") WHERE "status" = 'active';

CREATE TABLE "Answer" (
  "id"         TEXT NOT NULL PRIMARY KEY,
  "sessionId"  TEXT NOT NULL REFERENCES "StudySession"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "userId"     TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "wordId"     TEXT NOT NULL REFERENCES "Word"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "mode"       TEXT NOT NULL,
  "phase"      TEXT NOT NULL,
  "attempt"    INTEGER NOT NULL DEFAULT 1,
  "correct"    INTEGER NOT NULL,
  "userAnswer" TEXT,
  "hintUsed"   INTEGER NOT NULL DEFAULT 0,
  "dontKnow"   INTEGER NOT NULL DEFAULT 0,
  "durationMs" INTEGER NOT NULL DEFAULT 0,
  "dayKey"     TEXT NOT NULL,
  "createdAt"  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE UNIQUE INDEX "Answer_sessionId_wordId_mode_phase_attempt_key" ON "Answer"("sessionId", "wordId", "mode", "phase", "attempt");
CREATE INDEX "Answer_userId_dayKey_idx" ON "Answer"("userId", "dayKey");
CREATE INDEX "Answer_wordId_idx" ON "Answer"("wordId");

CREATE TABLE "Passage" (
  "id"        TEXT NOT NULL PRIMARY KEY,
  "userId"    TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "title"     TEXT NOT NULL,
  "titleCn"   TEXT,
  "body"      TEXT NOT NULL,
  "bodyCn"    TEXT,
  "questions" TEXT,
  "wordIds"   TEXT NOT NULL DEFAULT '[]',
  "source"    TEXT NOT NULL DEFAULT 'ai',
  "model"     TEXT,
  "createdAt" TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX "Passage_userId_createdAt_idx" ON "Passage"("userId", "createdAt");

CREATE TABLE "ReviewLog" (
  "id"              TEXT NOT NULL PRIMARY KEY,
  "userId"          TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "wordId"          TEXT NOT NULL REFERENCES "Word"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "sessionId"       TEXT REFERENCES "StudySession"("id") ON DELETE SET NULL ON UPDATE CASCADE,
  "rating"          INTEGER NOT NULL,
  "stateBefore"     INTEGER NOT NULL,
  "stabilityAfter"  REAL NOT NULL,
  "difficultyAfter" REAL NOT NULL,
  "dueAfter"        TEXT NOT NULL,
  "reviewedAt"      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "dayKey"          TEXT NOT NULL
);
CREATE UNIQUE INDEX "ReviewLog_sessionId_wordId_key" ON "ReviewLog"("sessionId", "wordId");
CREATE INDEX "ReviewLog_userId_dayKey_idx" ON "ReviewLog"("userId", "dayKey");
CREATE INDEX "ReviewLog_userId_wordId_idx" ON "ReviewLog"("userId", "wordId");

CREATE TABLE "AppSetting" (
  "key"         TEXT NOT NULL PRIMARY KEY,
  "value"       TEXT NOT NULL,
  "updatedById" TEXT,
  "updatedAt"   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
