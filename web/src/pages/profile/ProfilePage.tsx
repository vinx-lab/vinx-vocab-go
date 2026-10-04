import { Button, Card, Col, Form, Input, List, Popconfirm, Radio, Row, Tag, useApp, BgColorsOutlined, BookOutlined, TeamOutlined } from "@/ui";
import { TargetBooksEditor } from "@/pages/coverage/components";
import { ROLE_LABEL, THEME_PREFS, type ThemePref } from "@vinx/shared";
import { api, errorMessage } from "@/lib/api";
import { check, useIdentity } from "@/lib/auth";
import { THEME_LABEL } from "@/lib/theme";
import { useThemePref } from "@/components/ThemeProvider";
import { can } from "@/lib/perms";
import { useApi, useMutation, invalidate } from "@/lib/query";
import { PageHeader } from "@/components/ui";
import { useMyTargets } from "@/lib/targets";
import type { MyClass, MyTargetBooks, Paged, TargetBook } from "@/types";

/**
 * 我的目标词书（spec 0003 / 0008）：
 * - 没有班级：自己设置；
 * - 在班里且班级允许自主：班级的书锁定（「由班级 ×× 设置」，不能删除），下面可以追加自己的书，增删排序；
 * - 在班里但班级不允许自主：只读班级目标，自己设的保留但不生效（退出所有班级或重新允许后恢复）。
 */
function MyTargetBooksCard() {
  const { message } = useApp();
  const q = useMyTargets();
  const save = useMutation({
    mutationFn: (bookIds: string[]) => api.put<MyTargetBooks>("/me/target-books", { bookIds }),
    onSuccess: () => {
      message.success("目标词书已保存");
      void invalidate(["me", "target-books"]);
      void invalidate(["records"]);
      void invalidate(["plans"]);
      void invalidate(["today"]);
    },
    onError: (e) => message.error(errorMessage(e, "保存失败")),
  });
  const t = q.data;
  const fromClass = t?.source === "class";
  const classBooks = t?.books.filter((b) => b.source === "class") ?? [];
  const classIds = classBooks.map((b) => b.id);
  // 自己设的书里与班级重复的不在「我追加的」里显示（按班级算），保存时原样带上，免得被删掉
  const ownExtra = t?.ownBooks.filter((b) => !classIds.includes(b.id)) ?? [];
  const ownHidden = t?.ownBooks.filter((b) => classIds.includes(b.id)).map((b) => b.id) ?? [];
  const lockTag = (b: TargetBook) => (
    <span title="由班级设置，不能删除">
<Tag bordered={false} style={{ marginInlineEnd: 0, flexShrink: 0 }}>
      由班级 {(b.classNames ?? []).join("、")} 设置
    </Tag>
</span>
  );

  return (
    <Card
      title={
        <>
          <BookOutlined /> 目标词书
        </>
      }
    >
      {q.isLoading || !t ? (
        <div style={{ color: "var(--muted)" }}>{q.error ? errorMessage(q.error, "加载失败") : "加载中…"}</div>
      ) : !fromClass ? (
        <>
          <div style={{ marginBottom: 12, color: "var(--muted)", fontSize: 13 }}>
            这一阶段要求自己覆盖的词书。今日页的目标进度和学习记录按这些词书统计；自己建计划时只能从这些词书里选单元。
          </div>
          <TargetBooksEditor value={t.books} saving={save.isPending} onSave={(ids) => save.mutate(ids)} />
        </>
      ) : (
        <div style={{ display: "grid", gap: 16 }}>
          <div>
            <div style={{ marginBottom: 8, color: "var(--muted)", fontSize: 13 }}>
              由班级 {t.classes.map((c) => c.name).join("、")} 设置。
            </div>
            <TargetBooksEditor value={classBooks} readOnly rowExtra={lockTag} />
          </div>
          {t.canEditOwn ? (
            <div>
              <div style={{ marginBottom: 8, fontWeight: 600 }}>我追加的</div>
              <div style={{ marginBottom: 8, color: "var(--muted)", fontSize: 13 }}>
                班级允许自主安排：可以在班级目标之外追加词书，进度一起计入目标，也可以用它们自己建计划。
              </div>
              <TargetBooksEditor
                value={ownExtra}
                excludeIds={classIds}
                emptyText="还没有追加的词书，从下面添加"
                saving={save.isPending}
                onSave={(ids) => save.mutate([...ids, ...ownHidden])}
              />
            </div>
          ) : (
            <div style={{ color: "var(--muted)", fontSize: 13 }}>
              班级未开放自主安排，目标词书由老师设置。
              {ownExtra.length > 0 && `你之前追加的 ${ownExtra.length} 本词书保留，暂不计入目标。`}
            </div>
          )}
        </div>
      )}
    </Card>
  );
}

/** 个人中心：资料、外观、密码、我的班级（邀请码加入）、目标词书 */
export function ProfilePage() {
  const { data: identity, refetch } = useIdentity();
  const { message } = useApp();
  const [joinForm] = Form.useForm<{ inviteCode: string }>();
  const [pwdForm] = Form.useForm();
  const isLearner = can(identity, "study");
  const theme = useThemePref();

  const changeTheme = async (next: ThemePref) => {
    try {
      await theme.setPref(next);
      await refetch();
    } catch (e) {
      message.error(errorMessage(e, "外观保存失败"));
    }
  };

  const classes = useApi(["me", "classes"], () => api.get<Paged<MyClass>>("/me/classes"), { enabled: isLearner });

  const join = useMutation({
    mutationFn: (inviteCode: string) => api.post<{ name: string }>("/classes/join", { inviteCode }),
    onSuccess: (cls) => {
      message.success(`已加入「${cls.name}」`);
      joinForm.resetFields();
      void invalidate(["me"]);
      void invalidate(["today"]);
      void invalidate(["records"]);
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const leave = useMutation({
    mutationFn: (id: string) => api.del(`/me/classes/${id}`),
    onSuccess: () => {
      message.success("已退出班级");
      void invalidate(["me"]);
      void invalidate(["today"]);
      void invalidate(["records"]);
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  const saveProfile = async (values: { name: string; currentGrade?: string }) => {
    try {
      await api.put("/auth/profile", values);
      await check();
      message.success("资料已保存");
    } catch (e) {
      message.error(errorMessage(e));
    }
  };

  const changePassword = async (values: { oldPassword: string; newPassword: string; confirmPassword: string }) => {
    try {
      await api.post("/auth/change-password", values);
      pwdForm.resetFields();
      message.success("密码已修改");
    } catch (e) {
      message.error(errorMessage(e));
    }
  };

  return (
    <div className="vx-page">
      <PageHeader eyebrow="个人中心" title={identity?.name ?? "我的"}>
        账号 {identity?.email}
        {identity ? " · " : ""}
        {identity ? ROLE_LABEL[identity.role] : ""}
      </PageHeader>
      <Row gutter={[16, 16]}>
        {isLearner && (
          <Col xs={24} lg={12}>
            <Card
              title={
                <>
                  <TeamOutlined /> 我的班级
                </>
              }
            >
              <Form form={joinForm} layout="inline" onFinish={(v) => join.mutate(v.inviteCode)} style={{ marginBottom: 12, rowGap: 8 }}>
                <Form.Item name="inviteCode" rules={[{ required: true, message: "请输入邀请码" }]} style={{ flex: 1, minWidth: 160 }}>
                  <Input placeholder="输入 6 位班级邀请码" maxLength={10} style={{ textTransform: "uppercase", fontFamily: "var(--mono)" }} />
                </Form.Item>
                <Button type="primary" htmlType="submit" loading={join.isPending}>
                  加入
                </Button>
              </Form>
              <List
                loading={classes.isLoading}
                dataSource={classes.data?.items ?? []}
                locale={{ emptyText: "还没有加入班级" }}
                renderItem={(c) => (
                  <List.Item
                    actions={[
                      <Popconfirm key="leave" title="退出后将不再收到该班级的计划，学习记录会保留。" onConfirm={() => leave.mutate(c.id)}>
                        <Button type="link" size="small" danger>
                          退出
                        </Button>
                      </Popconfirm>,
                    ]}
                  >
                    <List.Item.Meta title={c.name} description={`${c.teacherName} 老师${c.archived ? " · 已归档" : ""}`} />
                  </List.Item>
                )}
              />
            </Card>
          </Col>
        )}
        {isLearner && (
          <Col xs={24} lg={12}>
            <MyTargetBooksCard />
          </Col>
        )}
        <Col xs={24} lg={12}>
          <Card title="基本资料">
            {identity && (
              <Form layout="vertical" initialValues={{ name: identity.name, currentGrade: identity.currentGrade ?? "" }} onFinish={saveProfile}>
                <Form.Item name="name" label="姓名" rules={[{ required: true, message: "请输入姓名" }]}>
                  <Input maxLength={50} />
                </Form.Item>
                <Form.Item name="currentGrade" label="年级（选填）">
                  <Input maxLength={20} placeholder="例如 八年级" />
                </Form.Item>
                <Button type="primary" htmlType="submit">
                  保存
                </Button>
              </Form>
            )}
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card
            title={
              <>
                <BgColorsOutlined /> 外观
              </>
            }
          >
            <Radio.Group
              aria-label="外观"
              optionType="button"
              buttonStyle="solid"
              value={theme.pref}
              disabled={theme.saving}
              onChange={(e) => changeTheme(e.target.value as ThemePref)}
              options={THEME_PREFS.map((p) => ({ value: p, label: THEME_LABEL[p] }))}
            />
            <div style={{ marginTop: 10, color: "var(--muted)", fontSize: 13 }}>跟着账号保存，换设备登录也一样。「跟随系统」会随手机或电脑的深色模式自动切换。</div>
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title="修改密码">
            <Form form={pwdForm} layout="vertical" onFinish={changePassword}>
              <Form.Item name="oldPassword" label="当前密码" rules={[{ required: true, message: "请输入当前密码" }]}>
                <Input.Password autoComplete="current-password" />
              </Form.Item>
              <Form.Item name="newPassword" label="新密码" rules={[{ required: true, min: 6, message: "至少 6 位" }]}>
                <Input.Password autoComplete="new-password" />
              </Form.Item>
              <Form.Item
                name="confirmPassword"
                label="确认新密码"
                dependencies={["newPassword"]}
                rules={[
                  { required: true, message: "请再次输入" },
                  ({ getFieldValue }) => ({
                    validator: (_, v) => (!v || v === getFieldValue("newPassword") ? Promise.resolve() : Promise.reject(new Error("两次输入不一致"))),
                  }),
                ]}
              >
                <Input.Password autoComplete="new-password" />
              </Form.Item>
              <Button htmlType="submit">修改密码</Button>
            </Form>
          </Card>
        </Col>
      </Row>
    </div>
  );
}
