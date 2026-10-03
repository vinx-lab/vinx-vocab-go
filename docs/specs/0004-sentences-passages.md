---
status: ready
---

# 内容模型：词 ← 句 ← 篇

## 问题

现在系统里只有「词」（单词、短语）这一层：

- 课本的重点句型、课文没有地方存放。老师要做「汉译英」句子默写，只能在系统外手工排版。
- AI 短文（K28）属于某个学生，正文和译文都是整段文字，没有逐句对照，所以句子不能拿来默写，也不能和词关联。
- 例句挂在词条上（`Word.example`），从一个词看不到它还出现在哪些句子、哪篇课文里。

单词、句子、短文应该连成一个整体：词组成句，句组成篇；学生可以从一个词找到用到它的句子，句子可以默写、批改、统计。

本 spec 只做内容模型和人工维护（导入、编辑、浏览）。AI 生成见 [0005](0005-ai-generation.md)，默写单与批改见 [0006](0006-dictation-sheets.md)。

## 方案

### 1. 三层结构

```
篇              单元的篇（UnitText）：课文、重点句型清单、句型仿写
                 学生的 AI 短文（Passage，现有）
  └─ 由多个句子按顺序组成（UnitTextSentence / PassageSentence）
句（Sentence）  一句英文 + 一句中文；最小的可默写、可批改单位
  └─ 自动关联句中出现的词（SentenceWord）
词（Word）      单词、短语（现有）
```

- **句子是原子**：一条句子是一对中英文，可以带一个「句型骨架」（例如 `I find ... useful.`），骨架用于仿写题。
- **篇是句子的有序容器**，分两种 `kind`：
  - `text`：连贯的课文或短文，阅读时按段落连起来显示；
  - `list`：重点句型这样的清单，一条一条显示。
- **两种容器，共用同一层句子**：
  - **单元的篇** `UnitText`（新表）：挂在单元下，所有能看这本词书的人共用，可见性和编辑权限都跟随词书（`VisibleBookFilter` / `AssertCanEditBook`）。创建人只做记录（删除账号时置空），不影响内容的存续。
  - **学生的 AI 短文** `Passage`（现有表）：仍属于生成它的学生，只有本人（以及能看这个学生记录的老师）能看。
  - 两种篇都按 `kind` 区分 `text` / `list`（AI 短文固定是 `text`），都通过各自的关联表引用 `Sentence`。默写单、阅读页、词 → 句反查只认「句子」，不关心句子来自哪种篇。

### 2. 数据

一次迁移，只新增表：

```sql
CREATE TABLE "Sentence" (
  "id"         TEXT NOT NULL PRIMARY KEY,
  "en"         TEXT NOT NULL,
  "cn"         TEXT NOT NULL,
  "frame"      TEXT,             -- 句型骨架，如 "I find ... useful."；可空
  "source"     TEXT NOT NULL,    -- import | manual | example | ai | variant
  "wordId"     TEXT REFERENCES "Word"("id") ON DELETE CASCADE,       -- 例句所属的词（source = example 时）
  "originId"   TEXT REFERENCES "Sentence"("id") ON DELETE SET NULL,  -- 仿写变式的原句（source = variant 时）
  "variantNote" TEXT,            -- 仿写改了什么，如「替换：making word cards → reading aloud」
  "model"      TEXT,             -- AI 生成时的模型名
  "createdById" TEXT REFERENCES "User"("id") ON DELETE SET NULL,
  "createdAt"  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "updatedAt"  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE "SentenceWord" (
  "sentenceId" TEXT NOT NULL REFERENCES "Sentence"("id") ON DELETE CASCADE,
  "wordId"     TEXT NOT NULL REFERENCES "Word"("id") ON DELETE CASCADE,
  "position"   INTEGER NOT NULL,   -- 在句中的第几个词（短语取起始位置）
  "form"       TEXT NOT NULL,      -- 句中的实际写法，如 went
  PRIMARY KEY ("sentenceId", "wordId", "position")
);
CREATE INDEX "SentenceWord_wordId_idx" ON "SentenceWord"("wordId");
CREATE TABLE "UnitText" (
  "id"          TEXT NOT NULL PRIMARY KEY,
  "unitId"      TEXT NOT NULL REFERENCES "Unit"("id") ON DELETE CASCADE ON UPDATE CASCADE,
  "kind"        TEXT NOT NULL DEFAULT 'text',   -- text | list
  "title"       TEXT NOT NULL,
  "titleCn"     TEXT,
  "sortOrder"   INTEGER NOT NULL DEFAULT 0,
  "createdById" TEXT REFERENCES "User"("id") ON DELETE SET NULL,
  "createdAt"   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  "updatedAt"   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE "UnitTextSentence" (
  "textId"     TEXT NOT NULL REFERENCES "UnitText"("id") ON DELETE CASCADE,
  "sentenceId" TEXT NOT NULL REFERENCES "Sentence"("id") ON DELETE CASCADE,
  "paragraph"  INTEGER NOT NULL DEFAULT 0,
  "sortOrder"  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY ("textId", "sentenceId")
);
CREATE TABLE "PassageSentence" (
  "passageId"  TEXT NOT NULL REFERENCES "Passage"("id") ON DELETE CASCADE,
  "sentenceId" TEXT NOT NULL REFERENCES "Sentence"("id") ON DELETE CASCADE,
  "paragraph"  INTEGER NOT NULL DEFAULT 0,
  "sortOrder"  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY ("passageId", "sentenceId")
);
```

- 单元内容不放进 `Passage`：`Passage.userId` 是 `NOT NULL ... ON DELETE CASCADE`（迁移只增不改，改不了），把共用的课文、句型挂在创建人名下，以后删除账号时会连带删掉。`UnitText` 的创建人用 `SET NULL`，与 `Book.ownerId` 一致。
- `Passage` 表结构不变，只增加关联表。`Passage.body` / `bodyCn` 保留，由句子拼出来，供旧接口和整篇显示使用；**句子是正本**。
- 句子被多个篇引用时（例如一条句型被仿写篇引用为原句），只在最后一个引用删除后才删句子；由服务层在删篇时清理没有引用的句子（例句除外，例句跟随词条）。
- `source` 取值：`import`（导入）、`manual`（手工录入）、`example`（例句）、`ai`（AI 生成的句型或短文）、`variant`（仿写变式）。

### 3. 例句迁移成句子

- 迁移时把每个有例句的词条（`Word.example` 非空）生成一条 `Sentence`（`source = example`，`wordId` 指向这个词），并计算 `SentenceWord`。
- `Word.example` / `exampleCn` **继续保留并同步写入**：现有接口、学习页、挖空题都不改。编辑例句时两边同时更新。
- 效果：上线后单词详情立刻就有「出现在这些句子里」，至少包含这个词自己的例句。

### 4. AI 短文迁移成逐句结构

- 已有的短文按句号、问号、叹号拆分英文和中文。两边句数一致时，逐句对应写入 `Sentence`（`source = ai`）和 `PassageSentence`；句数不一致时不拆，保留整段，界面上照旧显示，只是不能挑句子默写。
- 以后新生成的短文由 AI 直接按句输出（见 0005），不再需要拆分。

### 5. 词与句的自动关联（`internal/core/lemma`）

纯函数，配表驱动单测：

- **分词**：按空格和标点切开，保留撇号（`don't`、`o'clock`），统一转成小写比较。
- **词形还原**：
  - 规则变化：`-s` / `-es` / `-ies`，`-ed` / `-d` / `-ied`，双写辅音（`stopped`），`-ing`（去 e、双写），`-er` / `-est`。
  - 不规则变化：内置一张表（不规则动词、不规则复数、形容词比较级），放在 `data/` 下随程序嵌入。初稿从词库里带注释的拼写整理而来，例如 `go (went, gone)`、`child (复children)`。**要在「中考核心词汇」数据清洗之前先提取**，清洗会去掉这些注释。
- **短语匹配**：多词短语按词序匹配，`be` / `do` 可以变形（`be good at` 能匹配 `is good at`），`sb.` / `sth.` / `one's` 是占位符，可以匹配任意一个或几个词。
- **先长后短**：短语优先，被短语覆盖的位置不再单独匹配单词。
- **匹配范围**：全部词库（`Word`）。同一个拼写在词库里只有一条（K16），所以结果是确定的。
- 句子新增或修改时重新计算 `SentenceWord`。词库新增词条时不回头重算旧句子；提供一个管理员操作「重新关联全部句子」。

### 6. 超纲检查

- 对一个句子，给定一个「已知词集合」，列出句中**不在集合里**的词，即超纲词。
- 已知词集合的取法：
  - 学生视角：目标词（[0003](0003-target-books-coverage.md)）加上这个学生已有记忆状态的词；
  - 编辑单元时：这本词书到当前单元为止的词，再加上班级目标词书（如果有）。
- 结果只用于提示（标黄），不阻止保存。0005 的 AI 生成用它做生成后检查。
- 不认识的词（词库里根本没有）单独列为「词库外」，例如人名、地名。

### 7. 导入格式

在现有词表导入格式（`vocabparser`）里，`Unit N` 标题下面增加两种段落标记：

```
Unit 1
guitar /ɡɪˈtɑː(r)/ n. 吉他
...
[句型]
How do you learn English words? | 你是如何学习英文单词的？
I find ... useful. | 我发现……很有用。 | I find making word cards useful.
[课文]
Title: How I Learn English | 我是怎样学英语的
I used to find English hard. | 我过去觉得英语很难。
Then I started to read aloud every day. | 后来我开始每天大声朗读。

Now I can understand more. | 现在我能听懂更多了。
```

- `[句型]` 段：每行是「英文 | 中文」，可选第三列「完整例句」。英文里带「...」时，第二列中文对应骨架，第三列是完整句；默写时考完整句。
- `[课文]` 段：可选一行 `Title: 英文标题 | 中文标题`；每行一句「英文 | 中文」，**空行表示分段**。
- 遇到下一个 `Unit` 标题或新的段落标记时结束当前段。
- 预览页（现有的解析预览 → 修正 → 确认流程）增加「句型」「课文」两个标签，逐句显示解析结果和超纲词。
- 单元页面也可以单独「粘贴导入句型 / 课文」，格式与上面相同，只是不需要 `Unit` 标题。

### 8. 接口

| 接口 | 说明 |
|---|---|
| `GET /units/:id/texts` | 单元内的篇（课文、句型清单、仿写），带句子 |
| `POST /units/:id/texts` `{ title, titleCn?, kind, sentences: [{ en, cn, frame?, paragraph? }] }` | 新建篇。权限 `AssertCanEditBook` |
| `PATCH /texts/:id` | 改标题、`kind`、单元内顺序 |
| `PUT /texts/:id/sentences` `{ sentences: [...] }` | 整体替换句子列表（已有句子带 `id` 保留，否则新建），重新计算关联 |
| `DELETE /texts/:id` | 删除单元的篇 |
| `PATCH /units/:id/texts/order` | 单元内篇的排序 |
| `POST /units/:id/texts/import/preview`、`POST /units/:id/texts/import` | 单元内粘贴导入 |
| `GET /words/:id/sentences` | 出现过这个词的句子，按来源分组：例句、课本句型、课文、我的短文。学生只能看到自己有权看的 |
| `POST /sentences/analyze` `{ en, unitId? }` | 编辑时实时检查：返回分词、关联到的词、超纲词、词库外的词 |
| `GET /passages/:id` | 现有接口，响应增加 `sentences: [{ id, en, cn, paragraph, words: [{ wordId, position, form }] }]` |

现有 `/passages` 列表、生成接口的字段只增不改。

### 9. 界面

- **单元页面**（词书详情里选中一个单元）：顶部增加标签「词表 / 句型 / 课文」。
  - 句型：清单显示，每条有英文、中文、骨架；可以逐条编辑、拖动排序、删除；有编辑权限的人可以「粘贴导入」。
  - 课文：按段落显示，点一个句子可以看译文和逐词释义。
  - 编辑框下方实时显示超纲词和词库外的词。
- **单词详情**（`/words/:wordId` 及学习页的单词卡）：新增「出现在这些句子里」，最多显示 5 条，可以展开全部。句中的目标词高亮。
- **短文阅读页**：改成逐句结构后，点一句显示这一句的译文；整篇译文开关保留。
- 学生在单元页面也能看到句型和课文（只读），可以点读。

### 10. 权限

- 单元内容：查看跟随词书可见性，编辑跟随词书编辑权限（系统词书只有管理员能改，老师只能改自己的词书）。
- 学生的 AI 短文：保持现在的规则。

## 取舍

- **两种容器、一层句子**：考虑过把单元内容也存进 `Passage`（加 `unitId`），但 `Passage.userId` 是级联删除的必填外键，共用内容会随创建人的账号一起被删，而迁移只增不改，无法修改这个外键。所以单元内容用新表 `UnitText`，学生的 AI 短文继续用 `Passage`，两者共用 `Sentence`。默写单、阅读页、词 → 句的反查只认句子，不受两种容器的影响。
- **句子是正本、`body` 由句子拼出来**：保证默写、阅读、反查看到的是同一份内容。旧接口仍然能拿到整段文字。
- **例句双写**：迁移成句子以后仍然保留 `Word.example`，避免改动学习页、挖空题和契约测试。
- **句子不进 FSRS**：句子只记录正式测试的对错（见 0006），不参与记忆排程。以后要做句子复习再单独设计。
- **词形还原用规则 + 内置表**，不引入 NLP 库：词库是教材词汇，覆盖面有限，规则加表足够；单文件版也不宜引入大的依赖。

## 验证

- Go 单测：`core/lemma` 表驱动覆盖规则变化、不规则表、短语（含 be/do 变形和 sb./sth. 占位）、先长后短、撇号；`vocabparser` 覆盖 `[句型]`、`[课文]`、分段、三列句型、`Title:` 行、错误行提示；迁移测试覆盖例句转句子、AI 短文拆分（句数一致 / 不一致）。
- 契约测试：新增用例只对单文件版运行；`/passages/:id` 增加字段后，旧版对拍用例忽略该字段。新增单元篇（`/units/:id/texts`、`/texts/:id`）的增删改查、导入、`/words/:id/sentences`、`/sentences/analyze`，以及别人的单元内容 403、不可见词书 404。
- E2E：老师导入一个带句型和课文的单元 → 学生在单元页看到 → 学生在单词详情里看到这些句子。
- 用内置教材数据演练：导入后统计句子关联率（句中被关联到词的比例）。超纲词明显偏多时，检查不规则表和短语匹配是否有遗漏。

## 结果

（完成后补）
