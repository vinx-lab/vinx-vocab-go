import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import { useNavigate, useParams, useSearchParams } from "@/lib/router";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Button, Checkbox, Col, DatePicker, Divider, Empty, Input, InputNumber, Radio, Row, Segmented, Select, Space, Spin, Tag, useApp, CheckCircleFilled, TeamOutlined, UserOutlined } from "@/ui";
import dayjs from "dayjs";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import type { Book, BookDetail, ClassDetail, ClassItem, Mode, Paged, Plan, PlanInput, PlanKind } from "@/types";
import { MODE_LABEL } from "@/types";
import { ErrorBlock, Loading, PageHeader } from "@/components/ui";

type TargetMode = "self" | "class" | "user";
type UnitMeta = { name: string; bookId: string; bookName: string; sortOrder: number; wordCount?: number };
type PreviewData = { wordCount: number; units: { id: string; name: string; wordCount: number }[] };

const NAME_MAX = 60;

/** 自动生成计划名：词书名 + 单元范围 + 类型后缀 */
function autoName(unitIds: string[], meta: Record<string, UnitMeta>, kind: PlanKind): string {
  const suffix = kind === "daily" ? "每日背词" : "检测";
  const known = unitIds.map((id) => meta[id]).filter((m): m is UnitMeta => !!m);
  if (known.length === 0) return "";
  const books = [...new Set(known.map((m) => m.bookName))];
  let body: string;
  if (books.length > 1) {
    body = `${books[0]} 等 ${books.length} 本词书`;
  } else {
    const names = known.map((m) => m.name);
    body = `${books[0]} ${unitRange(names)}`;
  }
  return `${body} ${suffix}`.slice(0, NAME_MAX);
}

/** "Unit 1","Unit 2" → "Unit 1–2"；无规律时折叠 */
function unitRange(names: string[]): string {
  if (names.length === 1) return names[0];
  const parsed = names.map((n) => /^(.*?)(\d+)\s*$/.exec(n));
  if (parsed.every((p) => p && p[1] === parsed[0]![1])) {
    const nums = parsed.map((p) => Number(p![2])).sort((a, b) => a - b);
    const contiguous = nums.every((n, i) => i === 0 || n === nums[i - 1] + 1);
    if (contiguous) return `${parsed[0]![1]}${nums[0]}–${nums[nums.length - 1]}`;
  }
  return names.length <= 2 ? names.join("、") : `${names[0]} 等 ${names.length} 个单元`;
}

export function PlanEditorPage() {
  const { id } = useParams<{ id: string }>();
  const isEdit = !!id;
  const [search] = useSearchParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { message } = useApp();
  const { data: identity } = useIdentity();
  const canAssign = can(identity, "plans.assign");

  // ---------- 表单状态 ----------
  const [kind, setKind] = useState<PlanKind>("daily");
  const [name, setName] = useState("");
  const [nameTouched, setNameTouched] = useState(false);
  const [unitIds, setUnitIds] = useState<string[]>([]);
  const [unitMeta, setUnitMeta] = useState<Record<string, UnitMeta>>({});
  const [activeBookId, setActiveBookId] = useState<string | undefined>(undefined);
  const [targetMode, setTargetMode] = useState<TargetMode>("self");
  const [classIds, setClassIds] = useState<string[]>([]);
  const [userIds, setUserIds] = useState<string[]>([]);
  const [userNames, setUserNames] = useState<Record<string, string>>({});
  const [memberClassId, setMemberClassId] = useState<string | undefined>(undefined);
  const [newPerDay, setNewPerDay] = useState(10);
  const [reviewPerDay, setReviewPerDay] = useState(50);
  const [modes, setModes] = useState<Mode[]>(["recognition", "spelling"]);
  const [order, setOrder] = useState<"sequential" | "random">("sequential");
  const [testSize, setTestSize] = useState(20);
  const [testScope, setTestScope] = useState<"all" | "learned">("all");
  const [startDate, setStartDate] = useState<string | null>(null);
  const [endDate, setEndDate] = useState<string | null>(null);

  // ---------- 数据 ----------
  const planQuery = useQuery({
    queryKey: ["plans", "detail", id],
    queryFn: () => api.get<Plan>(`/plans/${id}`),
    enabled: isEdit,
  });

  const booksQuery = useQuery({
    queryKey: ["books", "list"],
    queryFn: () => api.get<Paged<Book>>("/books"),
  });

  const bookQuery = useQuery({
    queryKey: ["books", "detail", activeBookId],
    queryFn: () => api.get<BookDetail>(`/books/${activeBookId}`),
    enabled: !!activeBookId,
  });

  const classesQuery = useQuery({
    queryKey: ["classes", "list"],
    queryFn: () => api.get<Paged<ClassItem>>("/classes"),
    enabled: canAssign,
  });

  const membersQuery = useQuery({
    queryKey: ["classes", "detail", memberClassId],
    queryFn: () => api.get<ClassDetail>(`/classes/${memberClassId}`),
    enabled: canAssign && !!memberClassId,
  });

  const sortedKey = useMemo(() => [...unitIds].sort().join(","), [unitIds]);
  const previewQuery = useQuery({
    queryKey: ["plans", "preview", sortedKey],
    queryFn: () => api.post<PreviewData>("/plans/preview", { unitIds }),
    enabled: unitIds.length > 0,
    placeholderData: keepPreviousData,
  });
  const wordCount = unitIds.length > 0 ? (previewQuery.data?.wordCount ?? 0) : 0;

  // ---------- 初始化：新建（查询参数预填）/ 编辑（加载计划） ----------
  const initialized = useRef(false);
  useEffect(() => {
    if (initialized.current) return;
    if (isEdit) {
      const plan = planQuery.data;
      if (!plan) return;
      initialized.current = true;
      setKind(plan.kind);
      setName(plan.name);
      setNameTouched(true);
      setUnitIds(plan.units.map((u) => u.id));
      setUnitMeta(Object.fromEntries(plan.units.map((u, i) => [u.id, { name: u.name, bookId: u.bookId, bookName: u.bookName, sortOrder: i }])));
      setActiveBookId(plan.units[0]?.bookId);
      const cls = plan.targets.filter((t) => t.type === "class");
      const users = plan.targets.filter((t) => t.type === "user");
      setClassIds(cls.map((t) => t.id));
      setUserIds(plan.isSelfPlan ? [] : users.map((t) => t.id));
      setUserNames(Object.fromEntries(users.map((t) => [t.id, t.name])));
      setTargetMode(plan.isSelfPlan ? "self" : cls.length > 0 ? "class" : "user");
      setNewPerDay(plan.newPerDay);
      setReviewPerDay(plan.reviewPerDay);
      setModes(plan.modes);
      setOrder(plan.order);
      setTestSize(plan.testSize);
      setTestScope(plan.testScope);
      setStartDate(plan.startDate);
      setEndDate(plan.endDate);
    } else {
      initialized.current = true;
      const pre = (search.get("unitIds") ?? "").split(",").filter(Boolean);
      if (pre.length) setUnitIds(pre);
      const bookId = search.get("bookId");
      if (bookId) setActiveBookId(bookId);
      const classId = search.get("classId");
      if (classId) {
        setTargetMode("class");
        setClassIds([classId]);
      }
    }
  }, [isEdit, planQuery.data, search]);

  // 默认选中第一本词书
  useEffect(() => {
    if (!activeBookId && booksQuery.data?.items.length) setActiveBookId(booksQuery.data.items[0].id);
  }, [activeBookId, booksQuery.data]);

  // 载入词书详情时登记其单元信息（名称、顺序、词数），用于排序、芯片和自动命名
  useEffect(() => {
    const b = bookQuery.data;
    if (!b) return;
    setUnitMeta((prev) => {
      const next = { ...prev };
      for (const u of b.units) next[u.id] = { name: u.name, bookId: b.id, bookName: b.name, sortOrder: u.sortOrder, wordCount: u.wordCount };
      return next;
    });
  }, [bookQuery.data]);

  // 班级成员姓名登记
  useEffect(() => {
    const members = membersQuery.data?.members;
    if (!members) return;
    setUserNames((prev) => ({ ...prev, ...Object.fromEntries(members.map((m) => [m.id, m.name])) }));
  }, [membersQuery.data]);

  // 名称自动生成（用户手动修改后停止）
  const generatedName = useMemo(() => autoName(unitIds, unitMeta, kind), [unitIds, unitMeta, kind]);
  useEffect(() => {
    if (!nameTouched) setName(generatedName);
  }, [generatedName, nameTouched]);

  // ---------- 单元选择 ----------
  /** 保持“词书出现顺序 → 单元顺序”，计划按此顺序出新词 */
  function sortUnits(ids: string[]): string[] {
    const bookOrder: string[] = [];
    for (const uid of ids) {
      const b = unitMeta[uid]?.bookId;
      if (b && !bookOrder.includes(b)) bookOrder.push(b);
    }
    const rank = (uid: string) => {
      const m = unitMeta[uid];
      return m ? [bookOrder.indexOf(m.bookId), m.sortOrder] : [Number.MAX_SAFE_INTEGER, 0];
    };
    return [...ids].sort((a, b) => {
      const [ba, sa] = rank(a);
      const [bb, sb] = rank(b);
      return ba - bb || sa - sb;
    });
  }

  function toggleUnit(uid: string, checked: boolean) {
    setUnitIds((prev) => sortUnits(checked ? [...prev, uid] : prev.filter((x) => x !== uid)));
  }

  function toggleBookAll(checked: boolean) {
    const ids = bookQuery.data?.units.map((u) => u.id) ?? [];
    setUnitIds((prev) => sortUnits(checked ? [...new Set([...prev, ...ids])] : prev.filter((x) => !ids.includes(x))));
  }

  // ---------- 保存 ----------
  const save = useMutation({
    mutationFn: (body: Partial<PlanInput>) => (isEdit ? api.patch<Plan>(`/plans/${id}`, body) : api.post<Plan>("/plans", body)),
    onSuccess: (plan) => {
      queryClient.invalidateQueries({ queryKey: ["plans"] });
      queryClient.invalidateQueries({ queryKey: ["today"] });
      queryClient.invalidateQueries({ queryKey: ["classes"] });
      message.success(isEdit ? "计划已更新" : "计划已创建");
      navigate(`/plans/${plan.id}`);
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  function submit() {
    if (unitIds.length === 0) return message.warning("请至少选择一个单元");
    if (modes.length === 0) return message.warning("至少选择一种题型");
    if (!name.trim()) return message.warning("请填写计划名称");
    if (startDate && endDate && endDate < startDate) return message.warning("结束日期不能早于开始日期");
    const others = targetMode !== "self";
    if (canAssign && others && classIds.length === 0 && userIds.length === 0) return message.warning("请选择要安排的班级或学生");

    const body: Partial<PlanInput> = {
      name: name.trim(),
      kind,
      newPerDay,
      reviewPerDay,
      modes,
      order,
      testSize,
      testScope,
      startDate,
      endDate,
      unitIds,
    };
    // 无“为他人安排”权限：新建时空对象 = 自己；编辑时不改对象
    if (canAssign) body.targets = others ? { classIds, userIds } : { classIds: [], userIds: [] };
    else if (!isEdit) body.targets = { classIds: [], userIds: [] };
    save.mutate(body);
  }

  // ---------- 渲染 ----------
  if (isEdit && planQuery.isLoading) return <Loading />;
  if (isEdit && planQuery.error) return <ErrorBlock error={planQuery.error} onRetry={() => planQuery.refetch()} />;
  if (isEdit && planQuery.data && !planQuery.data.canEdit) {
    return (
      <div className="vx-page">
        <ErrorBlock error={new Error("只能修改自己创建的计划")} />
      </div>
    );
  }

  const book = bookQuery.data;
  const bookUnitIds = book?.units.map((u) => u.id) ?? [];
  const bookSelected = bookUnitIds.filter((x) => unitIds.includes(x)).length;
  const previewUnits = previewQuery.data?.units ?? [];
  const unitLabel = (uid: string) => {
    const m = unitMeta[uid];
    if (m) return `${m.bookName} · ${m.name}`;
    return previewUnits.find((u) => u.id === uid)?.name ?? "单元";
  };
  const classes = classesQuery.data?.items ?? [];
  const className = (cid: string) => classes.find((c) => c.id === cid)?.name ?? planQuery.data?.targets.find((t) => t.id === cid)?.name ?? "班级";

  return (
    <div className="vx-page">
      <PageHeader eyebrow={isEdit ? "Edit plan" : "New plan"} title={isEdit ? "编辑计划" : "新建计划"}>
        回答三个问题：给谁学、学什么、怎样学。
      </PageHeader>

      <Row gutter={[20, 20]}>
        <Col xs={24} lg={15}>
          <Space direction="vertical" size={16} style={{ width: "100%" }}>
            <Section index="0" title="类型">
              <Segmented<PlanKind>
                block
                value={kind}
                onChange={setKind}
                options={[
                  { label: "每日学习", value: "daily" },
                  { label: "一次检测", value: "test" },
                ]}
              />
              <Hint>{kind === "daily" ? "每天按额度学新词，并复习到期的词，适合长期背词。" : "从范围内抽词做一次测验，作答时不显示对错，交卷后出成绩。"}</Hint>
            </Section>

            <Section index="1" title="学什么">
              <Row gutter={[12, 12]} align="middle">
                <Col xs={24} sm={14}>
                  <Select
                    style={{ width: "100%" }}
                    placeholder="选择词书"
                    loading={booksQuery.isLoading}
                    value={activeBookId}
                    onChange={setActiveBookId}
                    showSearch
                    optionFilterProp="label"
                    options={(booksQuery.data?.items ?? []).map((b) => ({ value: b.id, label: `${b.name}${b.isSystem ? "（系统）" : ""}` }))}
                  />
                </Col>
                <Col xs={24} sm={10} style={{ textAlign: "right" }}>
                  {book && book.units.length > 0 && (
                    <Checkbox
                      checked={bookSelected === bookUnitIds.length}
                      indeterminate={bookSelected > 0 && bookSelected < bookUnitIds.length}
                      onChange={(e) => toggleBookAll(e.target.checked)}
                    >
                      全选本书（{book.units.length} 单元）
                    </Checkbox>
                  )}
                </Col>
              </Row>

              <div style={{ marginTop: 12, maxHeight: 300, overflowY: "auto", border: "1px solid var(--line)", borderRadius: 10, background: "var(--paper)" }}>
                {bookQuery.isLoading ? (
                  <div style={{ padding: 24, textAlign: "center" }}>
                    <Spin />
                  </div>
                ) : !book ? (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="先选择一本词书" />
                ) : book.units.length === 0 ? (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这本词书还没有单元" />
                ) : (
                  book.units.map((u) => {
                    const checked = unitIds.includes(u.id);
                    return (
                      <label
                        key={u.id}
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 10,
                          padding: "9px 12px",
                          borderBottom: "1px solid var(--line)",
                          cursor: "pointer",
                          background: checked ? "var(--primary-soft)" : undefined,
                        }}
                      >
                        <Checkbox checked={checked} onChange={(e) => toggleUnit(u.id, e.target.checked)} />
                        <span className="vx-word" style={{ flex: 1, minWidth: 0 }}>
                          {u.name}
                        </span>
                        <span style={{ color: "var(--muted)", fontSize: 12 }}>{u.wordCount} 词</span>
                      </label>
                    );
                  })
                )}
              </div>

              {unitIds.length > 0 && (
                <div style={{ marginTop: 12 }}>
                  <div style={{ fontSize: 13, color: "var(--muted)", marginBottom: 6 }}>已选 {unitIds.length} 个单元</div>
                  <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                    {unitIds.map((uid) => (
                      <Tag key={uid} closable onClose={() => toggleUnit(uid, false)} style={{ marginInlineEnd: 0 }}>
                        {unitLabel(uid)}
                      </Tag>
                    ))}
                  </div>
                </div>
              )}
              <div style={{ marginTop: 10, fontSize: 14 }}>
                共{" "}
                <span className="vx-num" style={{ fontSize: 20, fontWeight: 600, color: "var(--primary)" }}>
                  {wordCount}
                </span>{" "}
                个去重单词 {previewQuery.isFetching && <Spin size="small" style={{ marginLeft: 6 }} />}
              </div>
            </Section>

            <Section index="2" title="给谁学">
              {!canAssign ? (
                <div style={{ color: "var(--ink-soft)" }}>
                  <UserOutlined /> 为自己安排
                </div>
              ) : (
                <>
                  <Segmented<TargetMode>
                    value={targetMode}
                    onChange={setTargetMode}
                    options={[
                      { label: "自己", value: "self" },
                      { label: "班级", value: "class" },
                      { label: "学生", value: "user" },
                    ]}
                  />
                  {targetMode === "self" && <Hint>只安排给自己学习。</Hint>}
                  {targetMode === "class" && (
                    <div style={{ marginTop: 12 }}>
                      <Select
                        mode="multiple"
                        style={{ width: "100%" }}
                        placeholder="选择班级"
                        loading={classesQuery.isLoading}
                        value={classIds}
                        onChange={setClassIds}
                        optionFilterProp="label"
                        options={classes.map((c) => ({ value: c.id, label: `${c.name}（${c.memberCount} 人）${c.archived ? " · 已归档" : ""}` }))}
                      />
                      <Hint>之后加入班级的学生会自动获得这个计划。</Hint>
                    </div>
                  )}
                  {targetMode === "user" && (
                    <div style={{ marginTop: 12 }}>
                      <Select
                        style={{ width: "100%" }}
                        placeholder="先选择班级，再勾选学生"
                        loading={classesQuery.isLoading}
                        value={memberClassId}
                        onChange={setMemberClassId}
                        options={classes.map((c) => ({ value: c.id, label: `${c.name}（${c.memberCount} 人）` }))}
                      />
                      {memberClassId && (
                        <div style={{ marginTop: 8, maxHeight: 240, overflowY: "auto", border: "1px solid var(--line)", borderRadius: 10, padding: "6px 12px" }}>
                          {membersQuery.isLoading ? (
                            <Spin size="small" />
                          ) : (membersQuery.data?.members.length ?? 0) === 0 ? (
                            <span style={{ color: "var(--muted)" }}>该班级暂无学生</span>
                          ) : (
                            <Checkbox.Group
                              style={{ display: "flex", flexDirection: "column", gap: 6 }}
                              value={userIds}
                              onChange={(vals) => {
                                const inClass = new Set(membersQuery.data?.members.map((m) => m.id));
                                setUserIds((prev) => [...prev.filter((u) => !inClass.has(u)), ...(vals as string[])]);
                              }}
                              options={(membersQuery.data?.members ?? []).map((m) => ({ value: m.id, label: `${m.name}  ${m.email}` }))}
                            />
                          )}
                        </div>
                      )}
                    </div>
                  )}
                  {targetMode !== "self" && (classIds.length > 0 || userIds.length > 0) && (
                    <div style={{ marginTop: 12, display: "flex", flexWrap: "wrap", gap: 6 }}>
                      {classIds.map((cid) => (
                        <Tag key={cid} icon={<TeamOutlined />} closable onClose={() => setClassIds((p) => p.filter((x) => x !== cid))} style={{ marginInlineEnd: 0 }}>
                          {className(cid)}
                        </Tag>
                      ))}
                      {userIds.map((uid) => (
                        <Tag key={uid} icon={<UserOutlined />} closable onClose={() => setUserIds((p) => p.filter((x) => x !== uid))} style={{ marginInlineEnd: 0 }}>
                          {userNames[uid] ?? "学生"}
                        </Tag>
                      ))}
                    </div>
                  )}
                </>
              )}
            </Section>

            <Section index="3" title="怎样学">
              {kind === "daily" ? (
                <Row gutter={[16, 16]}>
                  <Col xs={24} sm={12}>
                    <FieldLabel>每天新词数</FieldLabel>
                    <Space wrap>
                      <InputNumber min={0} max={200} value={newPerDay} onChange={(v) => setNewPerDay(v ?? 0)} style={{ width: 96 }} />
                      {[5, 10, 20].map((n) => (
                        <Button key={n} size="small" type={newPerDay === n ? "primary" : "default"} onClick={() => setNewPerDay(n)}>
                          {n}
                        </Button>
                      ))}
                    </Space>
                  </Col>
                  <Col xs={24} sm={12}>
                    <FieldLabel>每天复习上限</FieldLabel>
                    <InputNumber min={0} max={500} value={reviewPerDay} onChange={(v) => setReviewPerDay(v ?? 0)} style={{ width: 96 }} />
                  </Col>
                  <Col xs={24} sm={12}>
                    <FieldLabel>题型</FieldLabel>
                    <ModesPicker value={modes} onChange={setModes} />
                  </Col>
                  <Col xs={24} sm={12}>
                    <FieldLabel>新词顺序</FieldLabel>
                    <Radio.Group value={order} onChange={(e) => setOrder(e.target.value)}>
                      <Radio value="sequential">按单元顺序</Radio>
                      <Radio value="random">随机</Radio>
                    </Radio.Group>
                  </Col>
                </Row>
              ) : (
                <Row gutter={[16, 16]}>
                  <Col xs={24} sm={12}>
                    <FieldLabel>抽取词数</FieldLabel>
                    <InputNumber min={1} max={500} value={testSize} onChange={(v) => setTestSize(v ?? 1)} style={{ width: 96 }} />
                  </Col>
                  <Col xs={24} sm={12}>
                    <FieldLabel>抽词范围</FieldLabel>
                    <Radio.Group value={testScope} onChange={(e) => setTestScope(e.target.value)}>
                      <Radio value="all">范围内全部</Radio>
                      <Radio value="learned">只测已学过的</Radio>
                    </Radio.Group>
                  </Col>
                  <Col xs={24}>
                    <FieldLabel>题型</FieldLabel>
                    <ModesPicker value={modes} onChange={setModes} />
                  </Col>
                </Row>
              )}
            </Section>

            <Section index="4" title="名称与日期" optional>
              <FieldLabel>计划名称</FieldLabel>
              <Input
                value={name}
                maxLength={NAME_MAX}
                showCount
                placeholder="选择单元后自动生成"
                onChange={(e) => {
                  setName(e.target.value);
                  setNameTouched(true);
                }}
              />
              {nameTouched && generatedName && name !== generatedName && (
                <Button
                  type="link"
                  size="small"
                  style={{ padding: 0 }}
                  onClick={() => {
                    setNameTouched(false);
                    setName(generatedName);
                  }}
                >
                  恢复自动命名
                </Button>
              )}
              <Row gutter={[16, 12]} style={{ marginTop: 12 }}>
                <Col xs={24} sm={12}>
                  <FieldLabel>开始日期</FieldLabel>
                  <DatePicker
                    style={{ width: "100%" }}
                    placeholder="立即开始"
                    value={startDate ? dayjs(startDate) : null}
                    onChange={(d) => setStartDate(d ? d.format("YYYY-MM-DD") : null)}
                  />
                </Col>
                <Col xs={24} sm={12}>
                  <FieldLabel>结束日期</FieldLabel>
                  <DatePicker
                    style={{ width: "100%" }}
                    placeholder="长期有效"
                    value={endDate ? dayjs(endDate) : null}
                    disabledDate={(d) => !!startDate && d.format("YYYY-MM-DD") < startDate}
                    onChange={(d) => setEndDate(d ? d.format("YYYY-MM-DD") : null)}
                  />
                </Col>
              </Row>
            </Section>
          </Space>
        </Col>

        <Col xs={24} lg={9}>
          <div style={{ position: "sticky", top: 16 }}>
            <Summary
              kind={kind}
              wordCount={wordCount}
              unitCount={unitIds.length}
              newPerDay={newPerDay}
              reviewPerDay={reviewPerDay}
              modes={modes}
              testSize={testSize}
              testScope={testScope}
              targetText={
                !canAssign || targetMode === "self"
                  ? "自己"
                  : [...classIds.map((c) => className(c)), ...userIds.map((u) => userNames[u] ?? "学生")].join("、") || "（未选择）"
              }
            />
            <Space style={{ marginTop: 16, width: "100%" }} direction="vertical">
              <Button type="primary" size="large" block loading={save.isPending} onClick={submit}>
                {isEdit ? "保存修改" : "创建计划"}
              </Button>
              <Button block onClick={() => navigate(-1)}>
                取消
              </Button>
            </Space>
          </div>
        </Col>
      </Row>
    </div>
  );
}

function Section({ index, title, optional, children }: { index: string; title: string; optional?: boolean; children: ComponentChildren }) {
  return (
    <section className="vx-card" style={{ padding: "16px 18px" }}>
      <div style={{ display: "flex", alignItems: "baseline", gap: 10, marginBottom: 12 }}>
        <span className="vx-num" style={{ color: "var(--accent)", fontSize: 18 }}>
          {index}
        </span>
        <h2 className="vx-title" style={{ fontSize: 18 }}>
          {title}
        </h2>
        {optional && <span style={{ color: "var(--muted)", fontSize: 12 }}>可选</span>}
      </div>
      {children}
    </section>
  );
}

function FieldLabel({ children }: { children: ComponentChildren }) {
  return <div style={{ fontSize: 13, color: "var(--ink-soft)", marginBottom: 6 }}>{children}</div>;
}

function Hint({ children }: { children: ComponentChildren }) {
  return <div style={{ fontSize: 12, color: "var(--muted)", marginTop: 8 }}>{children}</div>;
}

function ModesPicker({ value, onChange }: { value: Mode[]; onChange: (v: Mode[]) => void }) {
  return (
    <>
      <Checkbox.Group
        value={value}
        onChange={(v) => onChange(v as Mode[])}
        options={(["recognition", "spelling", "cloze"] as Mode[]).map((m) => ({ value: m, label: MODE_LABEL[m] }))}
      />
      {value.includes("cloze") && <Hint>挖空填词用例句出题；没有例句的单词会自动跳过这一题型（可在词书里用 AI 补例句）。</Hint>}
      {value.length === 0 && <div style={{ color: "var(--bad)", fontSize: 12, marginTop: 4 }}>至少选择一种题型</div>}
    </>
  );
}

function Summary(props: {
  kind: PlanKind;
  wordCount: number;
  unitCount: number;
  newPerDay: number;
  reviewPerDay: number;
  modes: Mode[];
  testSize: number;
  testScope: "all" | "learned";
  targetText: string;
}) {
  const { kind, wordCount, unitCount, newPerDay, reviewPerDay, modes, testSize, testScope, targetText } = props;
  const k = modes.length;
  const modeNames = modes.map((m) => MODE_LABEL[m]).join("+") || "（未选题型）";
  const days = newPerDay > 0 && wordCount > 0 ? Math.ceil(wordCount / newPerDay) : null;

  return (
    <div className="vx-card" style={{ padding: "18px 20px", background: "var(--surface)", borderTop: "4px solid var(--primary)" }}>
      <div className="vx-eyebrow">Summary</div>
      <h3 className="vx-title" style={{ fontSize: 18, marginTop: 2 }}>
        计划概要
      </h3>
      <Divider style={{ margin: "12px 0" }} />
      <div style={{ display: "flex", gap: 20, marginBottom: 12 }}>
        <SummaryNum label="单元" value={unitCount} />
        <SummaryNum label="去重单词" value={wordCount} />
        {kind === "daily" ? <SummaryNum label="预计天数" value={days ?? "—"} /> : <SummaryNum label="抽词" value={Math.min(testSize, wordCount || testSize)} />}
      </div>
      <div className="vx-cn" style={{ lineHeight: 1.9, color: "var(--ink)", fontSize: 14 }}>
        {unitCount === 0 ? (
          <p style={{ margin: 0, color: "var(--muted)" }}>先在“学什么”里选择单元。</p>
        ) : kind === "daily" ? (
          <>
            <p style={{ margin: 0 }}>
              每天最多新学 <b>{newPerDay}</b> 个词，复习到期词最多 <b>{reviewPerDay}</b> 个；每个词练 {modeNames} 共 <b>{k}</b> 题。
            </p>
            <p style={{ margin: "6px 0 0" }}>
              {days ? (
                <>
                  按 {wordCount} 个词计算，约 <b>{days}</b> 天学完。
                </>
              ) : newPerDay === 0 ? (
                "每天新词数为 0，只复习不学新词。"
              ) : null}
              新词每组最多 10 个，可连续学多组。
            </p>
            <p style={{ margin: "6px 0 0", color: "var(--ink-soft)" }}>缺席的日子新词额度不累计，到期复习会保留。</p>
          </>
        ) : (
          <>
            <p style={{ margin: 0 }}>
              从 {wordCount} 个词中{testScope === "learned" ? "已学过的词里" : ""}随机抽 <b>{testSize}</b> 个，每词 <b>{k}</b> 题（{modeNames}）。
            </p>
            <p style={{ margin: "6px 0 0", color: "var(--ink-soft)" }}>作答时不显示对错，交卷后出成绩。</p>
          </>
        )}
        <p style={{ margin: "10px 0 0", fontSize: 13, color: "var(--muted)" }}>
          <CheckCircleFilled style={{ color: "var(--primary)", marginRight: 6 }} />
          安排给：{targetText}
        </p>
      </div>
    </div>
  );
}

function SummaryNum({ label, value }: { label: string; value: ComponentChildren }) {
  return (
    <div>
      <div className="vx-num" style={{ fontSize: 26, fontWeight: 600, lineHeight: 1.1 }}>
        {value}
      </div>
      <div style={{ fontSize: 12, color: "var(--muted)" }}>{label}</div>
    </div>
  );
}
