-- 内容模型「词 ← 句 ← 篇」（spec 0004）：句子、句中词关联、单元的篇、篇与句子的关联、AI 短文与句子的关联。
-- 只新增表。例句转句子、已有 AI 短文的逐句拆分与词的关联需要分词和词形还原，
-- 由 Go 迁移钩子在同一事务里完成（internal/service/sentences.go 的 BackfillSentences）。

CREATE TABLE "Sentence" (
  "id"          TEXT NOT NULL PRIMARY KEY,
  "en"          TEXT NOT NULL,
  "cn"          TEXT NOT NULL,
  "frame"       TEXT,
  "source"      TEXT NOT NULL,
  "wordId"      TEXT REFERENCES "Word"("id") ON DELETE CASCADE,
  "originId"    TEXT REFERENCES "Sentence"("id") ON DELETE SET NULL,
  "variantNote" TEXT,
  "model"       TEXT,
  "createdById" TEXT REFERENCES "User"("id") ON DELETE SET NULL,
  "createdAt"   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "updatedAt"   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX "Sentence_wordId_idx" ON "Sentence"("wordId");
CREATE INDEX "Sentence_originId_idx" ON "Sentence"("originId");

CREATE TABLE "SentenceWord" (
  "sentenceId" TEXT NOT NULL REFERENCES "Sentence"("id") ON DELETE CASCADE,
  "wordId"     TEXT NOT NULL REFERENCES "Word"("id") ON DELETE CASCADE,
  "position"   INTEGER NOT NULL,
  "form"       TEXT NOT NULL,
  PRIMARY KEY ("sentenceId", "wordId", "position")
);
CREATE INDEX "SentenceWord_wordId_idx" ON "SentenceWord"("wordId");

CREATE TABLE "UnitText" (
  "id"          TEXT NOT NULL PRIMARY KEY,
  "unitId"      TEXT NOT NULL REFERENCES "Unit"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "kind"        TEXT NOT NULL DEFAULT 'text',
  "title"       TEXT NOT NULL,
  "titleCn"     TEXT,
  "sortOrder"   INTEGER NOT NULL DEFAULT 0,
  "createdById" TEXT REFERENCES "User"("id") ON DELETE SET NULL,
  "createdAt"   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "updatedAt"   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX "UnitText_unitId_sortOrder_idx" ON "UnitText"("unitId", "sortOrder");

CREATE TABLE "UnitTextSentence" (
  "textId"     TEXT NOT NULL REFERENCES "UnitText"("id") ON DELETE CASCADE,
  "sentenceId" TEXT NOT NULL REFERENCES "Sentence"("id") ON DELETE CASCADE,
  "paragraph"  INTEGER NOT NULL DEFAULT 0,
  "sortOrder"  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY ("textId", "sentenceId")
);
CREATE INDEX "UnitTextSentence_sentenceId_idx" ON "UnitTextSentence"("sentenceId");

CREATE TABLE "PassageSentence" (
  "passageId"  TEXT NOT NULL REFERENCES "Passage"("id") ON DELETE CASCADE,
  "sentenceId" TEXT NOT NULL REFERENCES "Sentence"("id") ON DELETE CASCADE,
  "paragraph"  INTEGER NOT NULL DEFAULT 0,
  "sortOrder"  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY ("passageId", "sentenceId")
);
CREATE INDEX "PassageSentence_sentenceId_idx" ON "PassageSentence"("sentenceId");
