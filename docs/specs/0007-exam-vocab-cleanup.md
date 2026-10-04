---
status: done
---

# 中考核心词汇清洗：带注释的拼写并回干净的词

## 问题

内置词书「中考核心词汇」来自一份两源合并的考纲词表，有 179 条拼写里带着注释，导入后成了独立的词：

| 写法 | 条数 | 例子 |
|---|---|---|
| 括号里的变形、说明 | 132 | `fly (flew, flown)`、`a (an)`、`begin(began,begun)`、`best（good, well的最高级）`、`metre (美meter)` |
| 双斜杠音标 | 30 | `gladness //ˈɡlædnəs//`、`bus-stop //ˈbʌs stɒp//` |
| 等号同义词 | 12 | `bike = bicycle`、`fridge =refrigerator`、`Mom =Mum` |
| 方括号音标 | 1 | `between [bɪˈtwiːn]` |

后果：

- 同一个词在库里有两个 `Word`：课本里的 `fly` 和核心词汇里的 `fly (flew, flown)`。在课本里学会、测过的词，在核心词汇里还是「没学过」「未测」。
- 目标词书按 `wordId` 去重，课本 + 核心词汇的分母被重复计数，虚高。
- 核心词汇书内也有重复（`begin` 和 `begin(began,begun)` 各一条），规范化后 2956 条只有 2887 个不同的词。
- 拼写题要求学生拼出 `fly (flew, flown)` 这样的字符串，题目本身是错的。

另外，正式数据里有一本从服务器版导入的「中考差集」，内容约等于「中考核心词汇 − 七上～九上 − 约 170 个基础功能词」，是当初为「课本学完只补中考新词」手工算出来的。清洗之后它不再需要（见「取舍」）。

## 方案

### 1. 拼写规整函数（纯函数）

`internal/core/vocabparser` 新增 `CleanSpelling(s) → (spelling, phonetic, note)`，只处理拼写**末尾**的注释：

| 规则 | 输入 | 输出 |
|---|---|---|
| 双斜杠音标 | `gladness //ˈɡlædnəs//` | `gladness`，音标 `ˈɡlædnəs` |
| 方括号音标 | `between [bɪˈtwiːn]` | `between`，音标 `bɪˈtwiːn` |
| 末尾括号（半角或全角，前面可以没有空格） | `fly (flew, flown)` | `fly`，注释 `flew, flown` |
| 等号 | `bike = bicycle` | `bike`，注释 `= bicycle` |

括号和等号两条规则只在剩下的部分是**单个词**（不含空格）时生效。像 `look after (sb.)` 这样的短语不动，避免误伤教材里有意写的短语。剩下的部分必须含字母，否则原样返回。

词表解析器在拆出拼写后调用它：音标为空时用拆出的音标，注释以「（…）」接在释义末尾。这样新安装导入时就是干净的，老师导入自己的词表也受益。

### 2. 源文件修正

`data/vocab/中考词汇_两源合并版.txt` 里解析器处理不了的个别行手工修正，例如 `mathematics` 那行的音标和释义错位，`begin` 那行释义开头的私有区字符。规则能处理的不改源文件，由解析器处理，保留来源原貌。

### 3. 已有库的修复（启动时，幂等）

参照 `BackfillSentences` 和 `BackfillBookLevels`，启动时在一个事务里执行 `RepairSystemWordSpellings`：

1. **范围**：只处理出现在系统词书（`isSystem = 1`）单元里的词。老师自己导入的词书不动。
2. 对每个范围内的词算 `CleanSpelling`，拼写没变的跳过。
3. 库里已有规整后拼写的词（大小写完全一致才算）：**合并**到那个词上，按下面的顺序。
4. 没有：直接改名，音标为空时补上拆出的音标，注释接在释义末尾。

**合并顺序**：`Word` 上的外键都是 `ON DELETE CASCADE`，所以必须先把所有引用改指向保留的词，最后才删除旧词。唯一索引冲突时的处理：

| 表 | 处理 | 冲突时 |
|---|---|---|
| `UnitWord` | 改指向 | 同一单元已有这个词：删掉旧的那条（保留 `sortOrder` 小的） |
| `MemoryState` | 改指向 | 同一学生两条都有：保留 `lastReview` 较晚的；相同时保留 `reps` 较多的 |
| `Answer` | 改指向 | 唯一键冲突：保留先写入的那条 |
| `ReviewLog` | 改指向 | 同一组同一词两条：保留先写入的那条 |
| `Sentence.wordId`（AI 例句） | 改指向 | 无唯一约束 |
| `SentenceWord` | 改指向 | 主键冲突：删掉旧的那条 |
| `WordSheet.wordIds`、`Passage.wordIds`（JSON 数组） | 解析后替换并去重，保持顺序 | — |
| `StudySession.snapshot`（JSON） | 按文本替换 id（cuid 全局唯一，替换安全） | — |

合并时保留词的拼写、音标、释义、例句都不变。修复完成后，范围内不应再有能被规整的拼写，所以第二次启动什么都不做。日志输出「改名 N、合并 M」。

### 4. 中考差集退役（不写代码）

清洗后，目标词书改用「课本 + 中考核心词汇」，从班级目标里去掉「中考差集」。这本书和用它建的计划都保留，照常能学，历史记录不受影响。这是数据操作，由管理员在界面上完成，不写迁移。

## 取舍

- **保留核心词汇、差集退役，而不是只留差集**：
  - 只留差集会漏掉约 170 个基础功能词，违反 K48「不设免测词」。
  - 差集依赖课本版本：以后加入九下或者换教材，就要重算。
  - 差集只有 1 个单元、2224 个词，建计划和出单词单都不好选。
  - 当初做差集是为了不重复学课本词。现在每日计划只从没有记忆状态的词里取新词，目标进度也按词去重。清洗之后，学完课本的人用核心词汇建计划，学到的自然就是「差集 + 功能词」。
- **不做派生词书**（自动算「核心 − 其他目标书」）：是一个新概念，现在没有必要。
- **修复只限系统词书**：老师自己的词表里，括号可能是有意写的，不替人改；新导入的由解析器规整。
- **大小写敏感匹配**：`May` 和 `may`、`China` 和 `china` 不能合并。
- **不清洗音标、释义层面的问题**：已有库只修拼写，源文件里个别行的音标、释义错位只影响新安装。

## 验证

- Go 单测：`CleanSpelling` 表驱动，覆盖上表四种写法、全角括号、无空格括号、短语不动、只剩符号时原样返回；解析器用例包含这几种行。
- 修复的单测：构造课本词 `fly` 和核心词 `fly (flew, flown)`，两边都有记忆状态、作答、复习记录、单元词、例句、单词单、学习组快照，并构造同一检测里两个词都答过的冲突。修复后检查每张表的行数和保留哪条，再跑一次确认幂等。
- 新安装：`seed-demo` 后，核心词汇里不再有带括号、斜杠、等号的单词拼写，课本词与核心词汇共用 `Word`。
- 真实数据演练：用一份正式数据的拷贝启动，比较修复前后各表的行数，差值必须等于预期的合并数；再看一个学生的目标进度和记录页。

## 结果

### 实际改动

- `internal/core/vocabparser/cleanspelling.go`：`CleanSpelling` 与 `AppendNote`。按方案的四条规则循环处理拼写末尾，直到没有可拆的部分（结果是不动点，启动修复的幂等依赖这一点）；多段注释按原顺序以「；」连接（`No.(缩) = number` → `No.`，注释 `缩；= number`）。比方案多两条：
  - 紧贴的一到三个小写字母（`toward(s)`、`mountain(s)`、`Olympic(s)`）注释写成完整的另一种拼写（`towards`），避免释义末尾出现「（s）」；
  - 缺右括号的半个括号（源文件断行留下的 `burn (-ed, -ed`、`smell (smelt, smelt`）也按末尾括号处理，同样只在剩下单个词时生效。
- 解析器 `parseLine`：拆出拼写后调用 `CleanSpelling`，类型按规整后的拼写重算（`fly (flew, flown)` 由 phrase 变为 word）；注释在「缺少中文释义」检查之后才接到释义末尾，只有注释的行仍判为 error。现有解析器用例无需修改。
- 源文件 `中考词汇_两源合并版.txt` 手工修正 21 行：
  - 去掉 11 行里的私有区字符（`begin`、`catch`、`beg`、`button`、`drink`、`knock`、`meal`、`praise`、`sweater`、`tea`、`win`），以及 `e-mail`、`such` 释义开头多余的 `]`；
  - 音标被拼写里的 `/` 截错位的 8 行改成括号或等号写法，交给规则处理：`mathematics = math, maths`、`grey (gray)`、`madam (madame)`、`p.m. (pm, P.M., PM)`、`U.N. (UN, the United Nations)`、`burn (-ed, -ed 或 burnt, burnt)`、`smell (smelt, smelt 或 -ed,-ed)`，以及 `practice(s)e` 改为 `practice (practise)`。
  - 其余 warning（如 `cheque /tʃek/ (美check) n.` 这类注释写在音标之后、释义开头）属于释义层面，未改。
- `internal/service/wordrepair.go`：`RepairSystemWordSpellings`（事务内）与打开钩子 `RepairWordSpellings`（`store.RegisterOpenHook`，与 `RepairBookLevels`、`RepairSentences` 同一位置；先只读查询，没有要修的词时不开写事务）。逐个处理并每次重新查目标词：两个脏拼写规整成同一个新词时，第一个改名，第二个合并到它上面。改名时类型按新拼写重算、清掉 `audioFile`（发音缓存按拼写命名）。合并按方案的表，补充：
  - `UnitWord` 冲突时留下的那条 `sortOrder` 取两者较小的；
  - `MemoryState` 的 `lastReview` 为空视为最早；两条完全相同时保留保留词的那条；
  - `Answer`、`ReviewLog` 的「先写入」按 `createdAt` / `reviewedAt`，相同再按 rowid；`ReviewLog.sessionId` 为空的不冲突；
  - 方案没列到、也存了词 id 的列：`WordSheet.items`（默写单题目，spec 0006；按下标批改，只替换不去重）、`StudySession.progress`、`StudySession.result`（与 `snapshot` 一样按文本替换）。文本替换连同引号一起匹配，只换完整的 JSON 字符串；
  - 日志：`系统词书拼写修复：改名 N、合并 M（合并时冲突去掉：单元词 …、记忆状态 …、作答 …、复习记录 …、句中词 …）`。
- 引用 `Word` 的表已按迁移文件逐一核对：`UnitWord`、`MemoryState`、`Answer`、`ReviewLog`、`Sentence.wordId`、`SentenceWord` 有外键；`WordSheet.wordIds`、`WordSheet.items`、`Passage.wordIds`、`StudySession.snapshot/progress/result` 是 JSON。
- `internal/seed` 测试：五本课本仍与旧 seed 导出的 `testdata/oracle-seed.tsv` 逐行一致；中考核心词汇改为与新增的 `testdata/core-seed.tsv` 比较（单文件版自己的快照，`UPDATE_CORE_SEED=1` 重新生成）。新增断言：系统词书里所有拼写都是 `CleanSpelling` 的不动点、核心词汇里没有带括号/斜杠/等号的拼写、`awake`/`fridge`/`throw`/`wolf` 由课本与核心词汇共用同一个 `Word`。新安装的计数：`Word` 3809 → 3730，`UnitWord` 4312 → 4245，核心词汇 2956 → 2889 个词。

### 验证

- `make vet`、`make test` 通过（PostgreSQL 导入测试未设 `VINX_TEST_PG_URL`，跳过；其夹具里的拼写都是干净的，不受影响）。
- `CleanSpelling` 表驱动单测：四种写法、全角括号、无空格括号、组合注释、紧贴可省字母、缺右括号、短语不动、中间括号不动、只剩符号原样返回，并逐条检查不动点；解析器用例覆盖这几种行与「只有注释不算释义」。
- 修复单测：课本 `fly` 与核心 `fly (flew, flown)` 两边都有单元词、记忆状态（三种冲突/不冲突情形）、作答（含同一学习组同一 mode/phase/attempt 的冲突）、复习记录（含 sessionId 为空）、例句、句中词（含主键冲突）、单词单 `wordIds`/`items`、短文 `wordIds`、学习组 `snapshot/progress/result`；另有一对规整成同一新词的脏拼写（先改名后合并）和一个只在老师词书里的脏拼写（不动）。逐表核对保留哪条、外键检查无误，第二次运行统计全为 0。打开钩子单测：重开数据库后改名生效，大小写不同（`Mom` 与 `mom`）不合并。
- 真实数据演练（正式数据的拷贝，经 `store.Open` 走生产路径；其他打开钩子在这份数据上没有要做的事，差值全部来自本修复）：
  - 范围内待规整 174 个词：改名 9、合并 165；合并时冲突去掉单元词 67 条，记忆状态、作答、复习记录、句中词均为 0（这 165 个旧词上没有学习记录，只有 11 条句中词关联，均已改指向）；
  - 行数：`Word` 3906 → 3741（−165，等于合并数），`UnitWord` 6543 → 6476（−67，等于单元词冲突数），`MemoryState` 2813、`Answer` 4275、`ReviewLog` 3643、`Sentence` 26、`SentenceWord` 190、`WordSheet` 3、`Passage` 2、`StudySession` 218 不变；外键检查、`integrity_check` 通过；没有词因合并而有两条例句句子；
  - 中考核心词汇 2956 → 2889 个不同的词（与新安装一致；「问题」一节的 2887 是动手前的估算，以实测为准）；「问题」一节的 179 条是按源文件数的，库里系统词书范围内实际带注释的拼写是 175 个（174 个可规整，加上 `practice(s)e`）；课本（七上～九上，1340 个词）+ 中考核心词汇去重后的目标词数 3816 → 3738；
  - 再打开一次：无日志、各表行数不变。

### 未验证与遗留

- 已有库里核心词汇的 `practice(s)e` 不在规则范围内（括号在词中间），修复后仍是独立的词；新安装因源文件已改为 `practice (practise)` 而合并到 `practice`。需要时由管理员在界面上处理。
- 改名的词，其句中词关联不重算（新拼写的词形与原句可能能多关联几句）；需要时用管理员操作「重新关联全部句子」。
- 学习组快照里内嵌的拼写、释义（`answer`、`spelling` 等）只替换了词 id，不改内容；演练数据里没有进行中的学习组引用这些词。
- 合入后主会话补做：用修复后的拷贝以新构建启动，启动日志「改名 9、合并 165」与演练一致；把班级目标换成课本五册 + 中考核心词汇（模拟差集退役）后，学生的目标进度为已测 2783 / 3738，浏览器里今日页和「我的」显示正常（见 0008 结果）。正式环境的差集退役仍待管理员在界面上操作。
