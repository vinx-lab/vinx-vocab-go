-- 班级是否允许学生自主安排计划（spec 0008）。已有班级保持原来的行为（允许）。
ALTER TABLE "Classroom" ADD COLUMN "allowSelfPlan" INTEGER NOT NULL DEFAULT 1;
