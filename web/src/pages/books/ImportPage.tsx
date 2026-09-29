import { useMemo, useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import { useNavigate, useSearchParams } from "@/lib/router";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Alert, Button, Checkbox, Col, Collapse, Form, Input, Radio, Result, Row, Select, Space, Steps, Switch, Table, Tag, Upload, useApp, ArrowLeftOutlined, FileTextOutlined, UploadOutlined } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import type { Book, BookDetail, ImportPreview, ImportPreviewEntry, Paged } from "@/types";
import { PageHeader, StatTile } from "@/components/ui";

type EditEntry = ImportPreviewEntry & { key: string; include: boolean };
type EditUnit = { key: string; name: string; entries: EditEntry[] };
type ImportResult = { bookId: string; units: number; unitsCreated: number; wordsLinked: number; wordsCreated: number; wordsReused: number };
type EditableField = "spelling" | "phonetic" | "partOfSpeech" | "definition";

const STATUS_TAG: Record<ImportPreviewEntry["status"], { color: string; label: string }> = {
  ok: { color: "green", label: "正常" },
  warning: { color: "gold", label: "需检查" },
  error: { color: "red", label: "错误" },
};

const SAMPLE = `Unit 1
dinosaur /'daɪnəsɔː(r)/ n. 恐龙
museum /mju'ziːəm/ n. 博物馆
be good at 擅长……

Unit 2
weather /'weðə(r)/ n. 天气`;

export function ImportPage() {
  const [search] = useSearchParams();
  const fixedBookId = search.get("bookId") ?? undefined;
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { message } = useApp();
  const { data: identity } = useIdentity();
  const isAdmin = identity?.role === "admin";

  const [step, setStep] = useState(0);
  // 第 1 步
  const [text, setText] = useState("");
  const [splitByInitial, setSplitByInitial] = useState(false);
  const [defaultUnit, setDefaultUnit] = useState("");
  // 第 2 步
  const [stats, setStats] = useState<ImportPreview["stats"] | null>(null);
  const [units, setUnits] = useState<EditUnit[]>([]);
  const [onlyIssues, setOnlyIssues] = useState(false);
  // 第 3 步
  const [targetType, setTargetType] = useState<"existing" | "new">("existing");
  const [targetBookId, setTargetBookId] = useState<string | undefined>(fixedBookId);
  const [newBook, setNewBook] = useState({ name: "", description: "", isSystem: false });
  const [result, setResult] = useState<ImportResult | null>(null);

  const fixedBookQuery = useQuery({
    queryKey: ["books", "detail", fixedBookId],
    queryFn: () => api.get<BookDetail>(`/books/${fixedBookId}`),
    enabled: !!fixedBookId,
  });
  const booksQuery = useQuery({
    queryKey: ["books", "list"],
    queryFn: () => api.get<Paged<Book>>("/books"),
    enabled: !fixedBookId,
  });
  const editableBooks = (booksQuery.data?.items ?? []).filter((b) => b.canEdit);
  const canCreateBook = can(identity, "books.edit");

  // ---------- 解析预览 ----------
  const preview = useMutation({
    mutationFn: () => api.post<ImportPreview>("/books/import/preview", { text, splitByInitial, defaultUnit: defaultUnit.trim() || undefined }),
    onSuccess: (data) => {
      setStats(data.stats);
      setUnits(
        data.units.map((u, ui) => ({
          key: `u${ui}`,
          name: u.name,
          entries: u.entries.map((e, ei) => ({ ...e, key: `u${ui}-${ei}`, include: e.status !== "error" })),
        })),
      );
      setOnlyIssues(false);
      if (data.stats.entries === 0) {
        message.warning("没有解析出任何词条，请检查格式");
        return;
      }
      setStep(1);
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  function readFile(file: File) {
    const reader = new FileReader();
    reader.onload = () => {
      setText(String(reader.result ?? ""));
      message.success(`已读取 ${file.name}`);
    };
    reader.onerror = () => message.error("读取文件失败");
    reader.readAsText(file, "UTF-8");
    return false; // 阻止自动上传
  }

  // ---------- 修正 ----------
  function updateEntry(unitKey: string, entryKey: string, patch: Partial<EditEntry>) {
    setUnits((prev) =>
      prev.map((u) => (u.key !== unitKey ? u : { ...u, entries: u.entries.map((e) => (e.key === entryKey ? { ...e, ...patch } : e)) })),
    );
  }
  function setUnitInclude(unitKey: string, include: boolean) {
    setUnits((prev) => prev.map((u) => (u.key !== unitKey ? u : { ...u, entries: u.entries.map((e) => ({ ...e, include })) })));
  }

  const payloadUnits = useMemo(
    () =>
      units
        .map((u) => ({
          name: u.name.trim(),
          entries: u.entries
            .filter((e) => e.include)
            .map((e) => ({
              spelling: e.spelling.trim(),
              phonetic: e.phonetic.trim() || null,
              partOfSpeech: e.partOfSpeech.trim() || null,
              definition: e.definition.trim(),
              type: e.type,
            })),
        }))
        .filter((u) => u.entries.length > 0),
    [units],
  );
  const includedCount = payloadUnits.reduce((s, u) => s + u.entries.length, 0);

  function goToTarget() {
    if (includedCount === 0) return message.warning("没有勾选任何词条");
    if (payloadUnits.some((u) => !u.name)) return message.warning("单元名称不能为空");
    const bad = payloadUnits.flatMap((u) => u.entries).find((e) => !e.spelling || !e.definition);
    if (bad) return message.warning(`已勾选的词条中有拼写或释义为空的（${bad.spelling || "空拼写"}），请修正或取消勾选`);
    setStep(2);
  }

  // ---------- 导入 ----------
  const doImport = useMutation({
    mutationFn: () => {
      const body =
        fixedBookId || targetType === "existing"
          ? { bookId: fixedBookId ?? targetBookId, units: payloadUnits }
          : {
              newBook: { name: newBook.name.trim(), description: newBook.description.trim() || null, ...(isAdmin ? { isSystem: newBook.isSystem } : {}) },
              units: payloadUnits,
            };
      return api.post<ImportResult>("/books/import", body);
    },
    onSuccess: (r) => {
      queryClient.invalidateQueries({ queryKey: ["books"] });
      queryClient.invalidateQueries({ queryKey: ["plans"] });
      setResult(r);
      setStep(3);
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  function submitImport() {
    if (!fixedBookId && targetType === "existing" && !targetBookId) return message.warning("请选择要导入的词书");
    if (!fixedBookId && targetType === "new" && !newBook.name.trim()) return message.warning("请填写新词书名称");
    doImport.mutate();
  }

  function reset() {
    setStep(0);
    setText("");
    setUnits([]);
    setStats(null);
    setResult(null);
  }

  const fixedBookName = fixedBookQuery.data?.name;

  return (
    <div className="vx-page">
      <PageHeader
        eyebrow="Import"
        title="导入词表"
        extra={
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(fixedBookId ? `/books/${fixedBookId}` : "/books")}>
            返回词书
          </Button>
        }
      >
        {fixedBookId ? `导入到「${fixedBookName ?? "…"}」` : "粘贴或上传 txt 词表，检查后导入到词书。"}
      </PageHeader>

      <div className="vx-card" style={{ padding: "14px 18px", marginBottom: 16 }}>
        <Steps
          current={Math.min(step, 2)}
          size="small"
          responsive
          items={[{ title: "粘贴或上传" }, { title: "检查修正" }, { title: "选择目标并导入" }]}
          status={step === 3 ? "finish" : undefined}
        />
      </div>

      {step === 0 && (
        <Row gutter={[16, 16]}>
          <Col xs={24} lg={15}>
            <div className="vx-card" style={{ padding: 16 }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10, gap: 8, flexWrap: "wrap" }}>
                <span style={{ fontWeight: 600 }}>词表内容</span>
                <Upload accept=".txt,text/plain" showUploadList={false} beforeUpload={readFile} maxCount={1}>
                  <Button icon={<UploadOutlined />}>上传 txt 文件</Button>
                </Upload>
              </div>
              <Input.TextArea
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder={SAMPLE}
                autoSize={{ minRows: 14, maxRows: 28 }}
                style={{ fontFamily: "var(--mono)", fontSize: 13 }}
              />
              <div style={{ fontSize: 12, color: "var(--muted)", marginTop: 6 }}>{text ? `${text.split(/\r?\n/).filter((l) => l.trim()).length} 行` : " "}</div>
              <Row gutter={[16, 12]} style={{ marginTop: 8 }} align="middle">
                <Col xs={24} sm={12}>
                  <Space>
                    <Switch checked={splitByInitial} onChange={setSplitByInitial} />
                    <span>无单元标题时按首字母分单元</span>
                  </Space>
                </Col>
                <Col xs={24} sm={12}>
                  <Input value={defaultUnit} onChange={(e) => setDefaultUnit(e.target.value)} maxLength={60} prefix={<span style={{ color: "var(--muted)" }}>默认单元名</span>} placeholder="未分组" />
                </Col>
              </Row>
              <Button type="primary" size="large" block style={{ marginTop: 16 }} disabled={!text.trim()} loading={preview.isPending} onClick={() => preview.mutate()}>
                解析预览
              </Button>
            </div>
          </Col>
          <Col xs={24} lg={9}>
            <div className="vx-card" style={{ padding: 16 }}>
              <div style={{ fontWeight: 600, marginBottom: 8 }}>
                <FileTextOutlined /> 格式说明
              </div>
              <ul style={{ paddingLeft: 18, margin: 0, lineHeight: 1.9, color: "var(--ink-soft)", fontSize: 13 }}>
                <li>
                  单词一行一个：<Code>dinosaur /'daɪnəsɔː(r)/ n. 恐龙</Code>
                </li>
                <li>
                  词组直接写中文释义：<Code>be good at 擅长……</Code>
                </li>
                <li>
                  单元标题单独一行：<Code>Unit 1</Code>，其后的词归入该单元
                </li>
                <li>音标、词性可省略；同一单元内重复的词会自动合并</li>
                <li>词库按拼写去重，已有的词会复用已有词条</li>
                <li>文件请使用 UTF-8 编码</li>
              </ul>
              <Button type="link" style={{ padding: 0, marginTop: 8 }} onClick={() => setText(SAMPLE)}>
                填入示例
              </Button>
            </div>
          </Col>
        </Row>
      )}

      {step === 1 && stats && (
        <>
          <Row gutter={[12, 12]} style={{ marginBottom: 16 }}>
            {[
              { label: "单元", value: stats.units },
              { label: "条目", value: stats.entries },
              { label: "正常", value: stats.ok, tone: "primary" as const },
              { label: "需检查", value: stats.warning, tone: "accent" as const },
              { label: "错误", value: stats.error, tone: "accent" as const },
              { label: "词库已有", value: stats.existing },
            ].map((s) => (
              <Col key={s.label} xs={8} sm={4}>
                <StatTile label={s.label} value={s.value} tone={s.tone} style={{ padding: "10px 12px" }} />
              </Col>
            ))}
          </Row>

          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8, marginBottom: 12 }}>
            <Space>
              <Switch checked={onlyIssues} onChange={setOnlyIssues} />
              <span>只看有问题的</span>
            </Space>
            <span style={{ color: "var(--ink-soft)" }}>
              已勾选 <b className="vx-num">{includedCount}</b> 条 · 错误条目默认不导入
            </span>
          </div>

          <Collapse
            defaultActiveKey={units.slice(0, 1).map((u) => u.key)}
            items={units
              .map((u) => {
                const visible = onlyIssues ? u.entries.filter((e) => e.status !== "ok") : u.entries;
                if (onlyIssues && visible.length === 0) return null;
                const inc = u.entries.filter((e) => e.include).length;
                const issues = u.entries.filter((e) => e.status !== "ok").length;
                return {
                  key: u.key,
                  label: (
                    <span>
                      <span className="vx-word" style={{ fontWeight: 600 }}>
                        {u.name || "（未命名）"}
                      </span>
                      <span style={{ color: "var(--muted)", fontSize: 12, marginLeft: 8 }}>
                        {u.entries.length} 条 · 已勾选 {inc}
                        {issues > 0 && <span style={{ color: "var(--accent)" }}> · {issues} 条待检查</span>}
                      </span>
                    </span>
                  ),
                  children: (
                    <UnitEditor
                      unit={u}
                      visible={visible}
                      onRename={(name) => setUnits((prev) => prev.map((x) => (x.key === u.key ? { ...x, name } : x)))}
                      onIncludeAll={(v) => setUnitInclude(u.key, v)}
                      onChange={(entryKey, patch) => updateEntry(u.key, entryKey, patch)}
                    />
                  ),
                };
              })
              .filter((x): x is NonNullable<typeof x> => x !== null)}
          />

          <div style={{ display: "flex", justifyContent: "space-between", marginTop: 16, gap: 8 }}>
            <Button onClick={() => setStep(0)}>上一步</Button>
            <Button type="primary" onClick={goToTarget}>
              下一步：选择目标（{includedCount} 条）
            </Button>
          </div>
        </>
      )}

      {step === 2 && (
        <div className="vx-card" style={{ padding: 18, maxWidth: 620 }}>
          <div style={{ marginBottom: 14, color: "var(--ink-soft)" }}>
            将导入 <b className="vx-num">{payloadUnits.length}</b> 个单元、<b className="vx-num">{includedCount}</b> 个词条。
          </div>
          {fixedBookId ? (
            <Alert type="info" showIcon message={`导入到词书「${fixedBookName ?? "…"}」`} description="与已有单元同名的会合并到该单元，已在单元中的词不会重复添加。" />
          ) : (
            <Form layout="vertical">
              <Form.Item label="导入到">
                <Radio.Group value={targetType} onChange={(e) => setTargetType(e.target.value)}>
                  <Radio value="existing">已有词书</Radio>
                  {canCreateBook && <Radio value="new">新建词书</Radio>}
                </Radio.Group>
              </Form.Item>
              {targetType === "existing" ? (
                <Form.Item label="选择词书" extra="只列出你可以编辑的词书；同名单元会合并。">
                  <Select
                    value={targetBookId}
                    onChange={setTargetBookId}
                    loading={booksQuery.isLoading}
                    placeholder={editableBooks.length || booksQuery.isLoading ? "选择词书" : "没有可编辑的词书，请新建"}
                    showSearch
                    optionFilterProp="label"
                    options={editableBooks.map((b) => ({ value: b.id, label: `${b.name}（${b.unitCount} 单元 · ${b.wordCount} 词）` }))}
                  />
                </Form.Item>
              ) : (
                <>
                  <Form.Item label="词书名称" required>
                    <Input value={newBook.name} maxLength={60} onChange={(e) => setNewBook((b) => ({ ...b, name: e.target.value }))} placeholder="例如：八年级上册" />
                  </Form.Item>
                  <Form.Item label="描述">
                    <Input.TextArea rows={2} maxLength={300} value={newBook.description} onChange={(e) => setNewBook((b) => ({ ...b, description: e.target.value }))} />
                  </Form.Item>
                  {isAdmin && (
                    <Form.Item label="设为系统词书" extra="系统词书对所有用户可见。">
                      <Switch checked={newBook.isSystem} onChange={(v) => setNewBook((b) => ({ ...b, isSystem: v }))} />
                    </Form.Item>
                  )}
                </>
              )}
            </Form>
          )}
          <div style={{ display: "flex", justifyContent: "space-between", marginTop: 16, gap: 8 }}>
            <Button onClick={() => setStep(1)}>上一步</Button>
            <Button type="primary" loading={doImport.isPending} onClick={submitImport}>
              确认导入
            </Button>
          </div>
        </div>
      )}

      {step === 3 && result && (
        <div className="vx-card" style={{ padding: 16 }}>
          <Result
            status="success"
            title="导入完成"
            subTitle={
              <span>
                导入 {result.units} 个单元（新建 {result.unitsCreated} 个），加入单词 {result.wordsLinked} 个；其中新建词条 {result.wordsCreated} 个，复用词库已有 {result.wordsReused} 个。
                {result.wordsLinked < result.wordsCreated + result.wordsReused && "部分词原本已在单元中，未重复添加。"}
              </span>
            }
            extra={[
              <Button key="open" type="primary" onClick={() => navigate(`/books/${result.bookId}`)}>
                打开词书
              </Button>,
              <Button key="again" onClick={reset}>
                继续导入
              </Button>,
            ]}
          />
        </div>
      )}
    </div>
  );
}

function Code({ children }: { children: ComponentChildren }) {
  return (
    <code style={{ fontFamily: "var(--mono)", fontSize: 12, background: "var(--paper-deep)", padding: "1px 5px", borderRadius: 4, wordBreak: "break-all" }}>
      {children}
    </code>
  );
}

function UnitEditor({
  unit,
  visible,
  onRename,
  onIncludeAll,
  onChange,
}: {
  unit: EditUnit;
  visible: EditEntry[];
  onRename: (name: string) => void;
  onIncludeAll: (v: boolean) => void;
  onChange: (entryKey: string, patch: Partial<EditEntry>) => void;
}) {
  const inc = unit.entries.filter((e) => e.include).length;
  const cell = (field: EditableField, placeholder: string, width: number, className?: string) => ({
    title: placeholder,
    dataIndex: field,
    width,
    render: (_: unknown, r: EditEntry) => (
      <Input
        size="small"
        className={className}
        value={r[field]}
        placeholder={placeholder}
        status={(field === "spelling" || field === "definition") && r.include && !r[field].trim() ? "error" : undefined}
        onChange={(e) => onChange(r.key, { [field]: e.target.value })}
      />
    ),
  });

  return (
    <div>
      <div style={{ display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap", marginBottom: 10 }}>
        <Input prefix={<span style={{ color: "var(--muted)" }}>单元名</span>} value={unit.name} maxLength={60} onChange={(e) => onRename(e.target.value)} style={{ maxWidth: 320 }} />
        <Checkbox checked={inc === unit.entries.length} indeterminate={inc > 0 && inc < unit.entries.length} onChange={(e) => onIncludeAll(e.target.checked)}>
          全部导入
        </Checkbox>
      </div>
      <Table<EditEntry>
        rowKey="key"
        size="small"
        dataSource={visible}
        pagination={visible.length > 50 ? { pageSize: 50, showSizeChanger: false, size: "small" } : false}
        scroll={{ x: 860 }}
        onRow={(r) => ({ style: { opacity: r.include ? 1 : 0.55 } })}
        columns={[
          {
            title: "导入",
            width: 56,
            fixed: "left",
            render: (_, r) => <Checkbox checked={r.include} onChange={(e) => onChange(r.key, { include: e.target.checked })} />,
          },
          {
            title: "状态",
            width: 150,
            render: (_, r) => (
              <div>
                <Tag color={STATUS_TAG[r.status].color} bordered={false}>
                  {STATUS_TAG[r.status].label}
                </Tag>
                {r.type === "phrase" && (
                  <Tag bordered={false} style={{ marginInlineEnd: 0 }}>
                    词组
                  </Tag>
                )}
                {r.issues.length > 0 && <div style={{ fontSize: 12, color: r.status === "error" ? "var(--bad)" : "var(--accent)", marginTop: 2 }}>{r.issues.join("；")}</div>}
                <div style={{ fontSize: 11, color: "var(--muted)", marginTop: 2 }} title={r.raw}>
                  第 {r.line} 行
                </div>
              </div>
            ),
          },
          cell("spelling", "拼写", 170, "vx-word"),
          cell("phonetic", "音标", 140),
          cell("partOfSpeech", "词性", 80),
          {
            ...cell("definition", "释义", 260),
            render: (_: unknown, r: EditEntry) => (
              <div>
                <Input
                  size="small"
                  value={r.definition}
                  placeholder="释义"
                  status={r.include && !r.definition.trim() ? "error" : undefined}
                  onChange={(e) => onChange(r.key, { definition: e.target.value })}
                />
                {r.existing && (
                  <div style={{ fontSize: 12, color: "var(--primary)", marginTop: 2 }}>词库已有：{r.existingDefinition}（将复用已有词条）</div>
                )}
              </div>
            ),
          },
        ]}
      />
    </div>
  );
}
