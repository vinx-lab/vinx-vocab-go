-- 目标词书（spec 0003）：班级目标由老师设置，个人目标由没有班级的人自己设置。
-- 覆盖状态不落库，每次请求现算（internal/core/coverage.go）。

CREATE TABLE "ClassTargetBook" (
  "classId"   TEXT NOT NULL REFERENCES "Classroom"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "bookId"    TEXT NOT NULL REFERENCES "Book"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "sortOrder" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY ("classId", "bookId")
);
CREATE TABLE "UserTargetBook" (
  "userId"    TEXT NOT NULL REFERENCES "User"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "bookId"    TEXT NOT NULL REFERENCES "Book"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "sortOrder" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY ("userId", "bookId")
);
