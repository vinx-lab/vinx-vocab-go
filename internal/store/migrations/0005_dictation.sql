-- 默写单（spec 0006）：单词单增加格式与题目列；句子题的批改结果写入 SentenceAnswer（句子不进 FSRS）。
-- format：selftest（对折自测表，默认）| dictation（默写单）；items：默写单的题目 JSON 数组 [{type, wordId?, sentenceId?}]。

ALTER TABLE "WordSheet" ADD COLUMN "format" TEXT NOT NULL DEFAULT 'selftest';
ALTER TABLE "WordSheet" ADD COLUMN "items" TEXT NOT NULL DEFAULT '[]';

CREATE TABLE "SentenceAnswer" (
  "id"         TEXT NOT NULL PRIMARY KEY,
  "sessionId"  TEXT NOT NULL REFERENCES "StudySession"("id") ON DELETE CASCADE,
  "userId"     TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE,
  "sentenceId" TEXT NOT NULL REFERENCES "Sentence"("id") ON DELETE CASCADE,
  "itemType"   TEXT NOT NULL,
  "correct"    INTEGER NOT NULL,
  "userAnswer" TEXT,
  "dayKey"     TEXT NOT NULL,
  "createdAt"  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX "SentenceAnswer_user_sentence_idx" ON "SentenceAnswer"("userId", "sentenceId", "createdAt");
CREATE INDEX "SentenceAnswer_sessionId_idx" ON "SentenceAnswer"("sessionId");
