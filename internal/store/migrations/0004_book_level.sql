-- 词书学段（spec 0005）：primary（小学）/ junior（初中）/ exam（中考冲刺），可空。
-- 已有的系统词书按书名回填（与 internal/core/ai/levels.go 的 LevelForBookName 一致；
-- 「中考差集」供从服务器版导入的同名词书使用）。

ALTER TABLE "Book" ADD COLUMN "level" TEXT;

UPDATE "Book" SET "level" = 'junior'
WHERE "isSystem" = 1 AND "level" IS NULL
  AND "name" IN ('七年级上册', '七年级下册', '八年级上册', '八年级下册', '九年级上册');

UPDATE "Book" SET "level" = 'exam'
WHERE "isSystem" = 1 AND "level" IS NULL
  AND "name" IN ('中考核心词汇', '中考差集');
