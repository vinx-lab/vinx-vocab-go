-- spec 0009：复习记录的来源。线上结算为 NULL；历史补算写 'backfill-0009'，便于回溯与撤销。
ALTER TABLE "ReviewLog" ADD COLUMN "source" TEXT;
