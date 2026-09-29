-- 旧版（Prisma）PostgreSQL 表结构：pg_dump --schema-only 生成，供导入测试建源库用。

CREATE TABLE public."Answer" (
    id text NOT NULL,
    "sessionId" text NOT NULL,
    "userId" text NOT NULL,
    "wordId" text NOT NULL,
    mode text NOT NULL,
    phase text NOT NULL,
    attempt integer DEFAULT 1 NOT NULL,
    correct boolean NOT NULL,
    "userAnswer" text,
    "hintUsed" boolean DEFAULT false NOT NULL,
    "durationMs" integer DEFAULT 0 NOT NULL,
    "dayKey" text NOT NULL,
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "dontKnow" boolean DEFAULT false NOT NULL
);

CREATE TABLE public."AppSetting" (
    key text NOT NULL,
    value jsonb NOT NULL,
    "updatedById" text,
    "updatedAt" timestamp(3) without time zone NOT NULL
);

CREATE TABLE public."Book" (
    id text NOT NULL,
    name text NOT NULL,
    description text,
    "isSystem" boolean DEFAULT false NOT NULL,
    "ownerId" text,
    "sortOrder" integer DEFAULT 0 NOT NULL,
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamp(3) without time zone NOT NULL
);

CREATE TABLE public."ClassMember" (
    "classId" text NOT NULL,
    "userId" text NOT NULL,
    "joinedAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE public."Classroom" (
    id text NOT NULL,
    name text NOT NULL,
    "teacherId" text NOT NULL,
    "inviteCode" text NOT NULL,
    archived boolean DEFAULT false NOT NULL,
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamp(3) without time zone NOT NULL
);

CREATE TABLE public."MemoryState" (
    id text NOT NULL,
    "userId" text NOT NULL,
    "wordId" text NOT NULL,
    due timestamp(3) without time zone NOT NULL,
    stability double precision NOT NULL,
    difficulty double precision NOT NULL,
    "elapsedDays" integer DEFAULT 0 NOT NULL,
    "scheduledDays" integer DEFAULT 0 NOT NULL,
    "learningSteps" integer DEFAULT 0 NOT NULL,
    reps integer DEFAULT 0 NOT NULL,
    lapses integer DEFAULT 0 NOT NULL,
    state integer DEFAULT 0 NOT NULL,
    "lastReview" timestamp(3) without time zone,
    "introducedAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "introducedPlanId" text,
    "introducedDay" text NOT NULL,
    "updatedAt" timestamp(3) without time zone NOT NULL
);

CREATE TABLE public."Passage" (
    id text NOT NULL,
    "userId" text NOT NULL,
    title text NOT NULL,
    "titleCn" text,
    body text NOT NULL,
    "bodyCn" text,
    questions jsonb,
    "wordIds" text[],
    source text DEFAULT 'ai'::text NOT NULL,
    model text,
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE public."Plan" (
    id text NOT NULL,
    name text NOT NULL,
    "creatorId" text NOT NULL,
    kind text DEFAULT 'daily'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    "newPerDay" integer DEFAULT 10 NOT NULL,
    "reviewPerDay" integer DEFAULT 50 NOT NULL,
    modes text[] DEFAULT ARRAY['recognition'::text, 'spelling'::text],
    "order" text DEFAULT 'sequential'::text NOT NULL,
    "testSize" integer DEFAULT 20 NOT NULL,
    "testScope" text DEFAULT 'all'::text NOT NULL,
    "startDate" text,
    "endDate" text,
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamp(3) without time zone NOT NULL
);

CREATE TABLE public."PlanTarget" (
    id text NOT NULL,
    "planId" text NOT NULL,
    "classId" text,
    "userId" text
);

CREATE TABLE public."PlanUnit" (
    "planId" text NOT NULL,
    "unitId" text NOT NULL,
    "sortOrder" integer DEFAULT 0 NOT NULL
);

CREATE TABLE public."ReviewLog" (
    id text NOT NULL,
    "userId" text NOT NULL,
    "wordId" text NOT NULL,
    "sessionId" text,
    rating integer NOT NULL,
    "stateBefore" integer NOT NULL,
    "stabilityAfter" double precision NOT NULL,
    "difficultyAfter" double precision NOT NULL,
    "dueAfter" timestamp(3) without time zone NOT NULL,
    "reviewedAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "dayKey" text NOT NULL
);

CREATE TABLE public."StudySession" (
    id text NOT NULL,
    "userId" text NOT NULL,
    "planId" text,
    kind text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    "dayKey" text NOT NULL,
    snapshot jsonb NOT NULL,
    progress jsonb,
    result jsonb,
    "startedAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "completedAt" timestamp(3) without time zone,
    "completedDay" text,
    "wordCount" integer DEFAULT 0 NOT NULL,
    "sheetId" text
);

CREATE TABLE public."Unit" (
    id text NOT NULL,
    "bookId" text NOT NULL,
    name text NOT NULL,
    "sortOrder" integer DEFAULT 0 NOT NULL,
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE public."UnitWord" (
    "unitId" text NOT NULL,
    "wordId" text NOT NULL,
    "sortOrder" integer DEFAULT 0 NOT NULL
);

CREATE TABLE public."User" (
    id text NOT NULL,
    email text NOT NULL,
    "passwordHash" text NOT NULL,
    name text NOT NULL,
    role text DEFAULT 'student'::text NOT NULL,
    "currentGrade" text,
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamp(3) without time zone NOT NULL,
    "createdById" text,
    theme text DEFAULT 'system'::text NOT NULL
);

CREATE TABLE public."Word" (
    id text NOT NULL,
    spelling text NOT NULL,
    type text DEFAULT 'word'::text NOT NULL,
    phonetic text,
    "partOfSpeech" text,
    definition text NOT NULL,
    example text,
    "exampleCn" text,
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updatedAt" timestamp(3) without time zone NOT NULL,
    "audioFile" text,
    "exampleAt" timestamp(3) without time zone,
    "exampleSource" text
);

CREATE TABLE public."WordSheet" (
    id text NOT NULL,
    "userId" text NOT NULL,
    "creatorId" text NOT NULL,
    seq integer NOT NULL,
    "wordIds" text[],
    modes text[] DEFAULT ARRAY['recognition'::text, 'spelling'::text],
    "createdAt" timestamp(3) without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE public._prisma_migrations (
    id character varying(36) NOT NULL,
    checksum character varying(64) NOT NULL,
    finished_at timestamp with time zone,
    migration_name character varying(255) NOT NULL,
    logs text,
    rolled_back_at timestamp with time zone,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    applied_steps_count integer DEFAULT 0 NOT NULL
);

ALTER TABLE ONLY public."Answer"
    ADD CONSTRAINT "Answer_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."AppSetting"
    ADD CONSTRAINT "AppSetting_pkey" PRIMARY KEY (key);

ALTER TABLE ONLY public."Book"
    ADD CONSTRAINT "Book_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."ClassMember"
    ADD CONSTRAINT "ClassMember_pkey" PRIMARY KEY ("classId", "userId");

ALTER TABLE ONLY public."Classroom"
    ADD CONSTRAINT "Classroom_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."MemoryState"
    ADD CONSTRAINT "MemoryState_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."Passage"
    ADD CONSTRAINT "Passage_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."PlanTarget"
    ADD CONSTRAINT "PlanTarget_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."PlanUnit"
    ADD CONSTRAINT "PlanUnit_pkey" PRIMARY KEY ("planId", "unitId");

ALTER TABLE ONLY public."Plan"
    ADD CONSTRAINT "Plan_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."ReviewLog"
    ADD CONSTRAINT "ReviewLog_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."StudySession"
    ADD CONSTRAINT "StudySession_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."UnitWord"
    ADD CONSTRAINT "UnitWord_pkey" PRIMARY KEY ("unitId", "wordId");

ALTER TABLE ONLY public."Unit"
    ADD CONSTRAINT "Unit_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."User"
    ADD CONSTRAINT "User_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."WordSheet"
    ADD CONSTRAINT "WordSheet_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public."Word"
    ADD CONSTRAINT "Word_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY public._prisma_migrations
    ADD CONSTRAINT _prisma_migrations_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX "Answer_sessionId_wordId_mode_phase_attempt_key" ON public."Answer" USING btree ("sessionId", "wordId", mode, phase, attempt);

CREATE INDEX "Answer_userId_dayKey_idx" ON public."Answer" USING btree ("userId", "dayKey");

CREATE INDEX "Answer_wordId_idx" ON public."Answer" USING btree ("wordId");

CREATE INDEX "Book_ownerId_idx" ON public."Book" USING btree ("ownerId");

CREATE INDEX "ClassMember_userId_idx" ON public."ClassMember" USING btree ("userId");

CREATE UNIQUE INDEX "Classroom_inviteCode_key" ON public."Classroom" USING btree ("inviteCode");

CREATE INDEX "Classroom_teacherId_idx" ON public."Classroom" USING btree ("teacherId");

CREATE INDEX "MemoryState_userId_due_idx" ON public."MemoryState" USING btree ("userId", due);

CREATE INDEX "MemoryState_userId_introducedPlanId_introducedDay_idx" ON public."MemoryState" USING btree ("userId", "introducedPlanId", "introducedDay");

CREATE UNIQUE INDEX "MemoryState_userId_wordId_key" ON public."MemoryState" USING btree ("userId", "wordId");

CREATE INDEX "Passage_userId_createdAt_idx" ON public."Passage" USING btree ("userId", "createdAt");

CREATE INDEX "PlanTarget_classId_idx" ON public."PlanTarget" USING btree ("classId");

CREATE UNIQUE INDEX "PlanTarget_planId_classId_key" ON public."PlanTarget" USING btree ("planId", "classId");

CREATE UNIQUE INDEX "PlanTarget_planId_userId_key" ON public."PlanTarget" USING btree ("planId", "userId");

CREATE INDEX "PlanTarget_userId_idx" ON public."PlanTarget" USING btree ("userId");

CREATE INDEX "PlanUnit_unitId_idx" ON public."PlanUnit" USING btree ("unitId");

CREATE INDEX "Plan_creatorId_idx" ON public."Plan" USING btree ("creatorId");

CREATE UNIQUE INDEX "ReviewLog_sessionId_wordId_key" ON public."ReviewLog" USING btree ("sessionId", "wordId");

CREATE INDEX "ReviewLog_userId_dayKey_idx" ON public."ReviewLog" USING btree ("userId", "dayKey");

CREATE INDEX "ReviewLog_userId_wordId_idx" ON public."ReviewLog" USING btree ("userId", "wordId");

CREATE UNIQUE INDEX "StudySession_active_uniq" ON public."StudySession" USING btree ("userId", COALESCE("planId", ''::text), kind) WHERE (status = 'active'::text);

CREATE INDEX "StudySession_planId_idx" ON public."StudySession" USING btree ("planId");

CREATE INDEX "StudySession_sheetId_idx" ON public."StudySession" USING btree ("sheetId");

CREATE INDEX "StudySession_userId_completedDay_idx" ON public."StudySession" USING btree ("userId", "completedDay");

CREATE INDEX "StudySession_userId_status_idx" ON public."StudySession" USING btree ("userId", status);

CREATE INDEX "UnitWord_unitId_sortOrder_idx" ON public."UnitWord" USING btree ("unitId", "sortOrder");

CREATE INDEX "UnitWord_wordId_idx" ON public."UnitWord" USING btree ("wordId");

CREATE UNIQUE INDEX "Unit_bookId_name_key" ON public."Unit" USING btree ("bookId", name);

CREATE INDEX "Unit_bookId_sortOrder_idx" ON public."Unit" USING btree ("bookId", "sortOrder");

CREATE UNIQUE INDEX "User_email_key" ON public."User" USING btree (email);

CREATE INDEX "WordSheet_creatorId_idx" ON public."WordSheet" USING btree ("creatorId");

CREATE UNIQUE INDEX "WordSheet_userId_seq_key" ON public."WordSheet" USING btree ("userId", seq);

CREATE UNIQUE INDEX "Word_spelling_key" ON public."Word" USING btree (spelling);

ALTER TABLE ONLY public."Answer"
    ADD CONSTRAINT "Answer_sessionId_fkey" FOREIGN KEY ("sessionId") REFERENCES public."StudySession"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."Answer"
    ADD CONSTRAINT "Answer_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."Answer"
    ADD CONSTRAINT "Answer_wordId_fkey" FOREIGN KEY ("wordId") REFERENCES public."Word"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."Book"
    ADD CONSTRAINT "Book_ownerId_fkey" FOREIGN KEY ("ownerId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE SET NULL;

ALTER TABLE ONLY public."ClassMember"
    ADD CONSTRAINT "ClassMember_classId_fkey" FOREIGN KEY ("classId") REFERENCES public."Classroom"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."ClassMember"
    ADD CONSTRAINT "ClassMember_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."Classroom"
    ADD CONSTRAINT "Classroom_teacherId_fkey" FOREIGN KEY ("teacherId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."MemoryState"
    ADD CONSTRAINT "MemoryState_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."MemoryState"
    ADD CONSTRAINT "MemoryState_wordId_fkey" FOREIGN KEY ("wordId") REFERENCES public."Word"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."Passage"
    ADD CONSTRAINT "Passage_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."PlanTarget"
    ADD CONSTRAINT "PlanTarget_classId_fkey" FOREIGN KEY ("classId") REFERENCES public."Classroom"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."PlanTarget"
    ADD CONSTRAINT "PlanTarget_planId_fkey" FOREIGN KEY ("planId") REFERENCES public."Plan"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."PlanTarget"
    ADD CONSTRAINT "PlanTarget_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."PlanUnit"
    ADD CONSTRAINT "PlanUnit_planId_fkey" FOREIGN KEY ("planId") REFERENCES public."Plan"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."PlanUnit"
    ADD CONSTRAINT "PlanUnit_unitId_fkey" FOREIGN KEY ("unitId") REFERENCES public."Unit"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."Plan"
    ADD CONSTRAINT "Plan_creatorId_fkey" FOREIGN KEY ("creatorId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."ReviewLog"
    ADD CONSTRAINT "ReviewLog_sessionId_fkey" FOREIGN KEY ("sessionId") REFERENCES public."StudySession"(id) ON UPDATE CASCADE ON DELETE SET NULL;

ALTER TABLE ONLY public."ReviewLog"
    ADD CONSTRAINT "ReviewLog_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."ReviewLog"
    ADD CONSTRAINT "ReviewLog_wordId_fkey" FOREIGN KEY ("wordId") REFERENCES public."Word"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."StudySession"
    ADD CONSTRAINT "StudySession_planId_fkey" FOREIGN KEY ("planId") REFERENCES public."Plan"(id) ON UPDATE CASCADE ON DELETE SET NULL;

ALTER TABLE ONLY public."StudySession"
    ADD CONSTRAINT "StudySession_sheetId_fkey" FOREIGN KEY ("sheetId") REFERENCES public."WordSheet"(id) ON UPDATE CASCADE ON DELETE SET NULL;

ALTER TABLE ONLY public."StudySession"
    ADD CONSTRAINT "StudySession_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."UnitWord"
    ADD CONSTRAINT "UnitWord_unitId_fkey" FOREIGN KEY ("unitId") REFERENCES public."Unit"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."UnitWord"
    ADD CONSTRAINT "UnitWord_wordId_fkey" FOREIGN KEY ("wordId") REFERENCES public."Word"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."Unit"
    ADD CONSTRAINT "Unit_bookId_fkey" FOREIGN KEY ("bookId") REFERENCES public."Book"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."WordSheet"
    ADD CONSTRAINT "WordSheet_creatorId_fkey" FOREIGN KEY ("creatorId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

ALTER TABLE ONLY public."WordSheet"
    ADD CONSTRAINT "WordSheet_userId_fkey" FOREIGN KEY ("userId") REFERENCES public."User"(id) ON UPDATE CASCADE ON DELETE CASCADE;

