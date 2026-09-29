import { useState } from "preact/hooks";
import { useNavigate } from "@/lib/router";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import { useIdentity } from "@/lib/auth";
import { Button, Col, Form, Input, Modal, Row, Switch, Tag, useApp, ImportOutlined, PlusOutlined } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { can } from "@/lib/perms";
import type { Book, Paged } from "@/types";
import { EmptyBlock, ErrorBlock, Loading, PageHeader } from "@/components/ui";

type BookForm = { name: string; description?: string; isSystem?: boolean };

export function BooksPage() {
  const navigate = useNavigate();
  const { data: identity } = useIdentity();
  const [creating, setCreating] = useState(false);

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["books", "list"],
    queryFn: () => api.get<Paged<Book>>("/books"),
  });

  const canCreate = can(identity, "books.edit");
  const canImport = can(identity, "books.edit");

  return (
    <div className="vx-page">
      <PageHeader
        eyebrow="Word books"
        title="词书"
        extra={
          <>
            {canImport && (
              <Button icon={<ImportOutlined />} onClick={() => navigate("/books/import")}>
                导入词表
              </Button>
            )}
            {canCreate && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>
                新建词书
              </Button>
            )}
          </>
        }
      >
        浏览单元词表，选中单元即可一键建学习计划。
      </PageHeader>

      {isLoading ? (
        <Loading />
      ) : error ? (
        <ErrorBlock error={error} onRetry={() => refetch()} />
      ) : !data || data.items.length === 0 ? (
        <EmptyBlock
          title="还没有词书"
          description={canImport ? "新建一本词书，或直接粘贴词表导入。" : "请联系老师或管理员添加词书。"}
          action={
            canImport && (
              <Button type="primary" onClick={() => navigate("/books/import")}>
                导入词表
              </Button>
            )
          }
        />
      ) : (
        <Row gutter={[16, 16]}>
          {data.items.map((b, i) => (
            <Col key={b.id} xs={12} sm={8} md={6}>
              <BookCover book={b} delay={i} onOpen={() => navigate(`/books/${b.id}`)} />
            </Col>
          ))}
        </Row>
      )}

      {creating && <CreateBookModal isAdmin={identity?.role === "admin"} onClose={() => setCreating(false)} onCreated={(id) => navigate(`/books/${id}`)} />}
    </div>
  );
}

function BookCover({ book, delay, onOpen }: { book: Book; delay: number; onOpen: () => void }) {
  return (
    <div
      className="vx-card vx-rise"
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      style={{
        cursor: "pointer",
        height: "100%",
        minHeight: 190,
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        position: "relative",
        animationDelay: `${Math.min(delay, 12) * 35}ms`,
      }}
    >
      {/* 书脊色带 */}
      <div style={{ position: "absolute", left: 0, top: 0, bottom: 0, width: 8, background: book.isSystem ? "var(--primary)" : "var(--accent)" }} />
      <div style={{ padding: "16px 14px 14px 22px", display: "flex", flexDirection: "column", flex: 1, background: "linear-gradient(180deg, var(--surface), var(--paper))" }}>
        <div>
          <Tag color={book.isSystem ? "cyan" : "volcano"} bordered={false} style={{ marginInlineEnd: 0 }}>
            {book.isSystem ? "系统" : book.canEdit ? "我的" : (book.ownerName ?? "自建")}
          </Tag>
        </div>
        <h3 className="vx-title" style={{ fontSize: 17, lineHeight: 1.35, marginTop: 10, wordBreak: "break-word" }}>
          {book.name}
        </h3>
        {book.description && (
          <div
            style={{
              fontSize: 12,
              color: "var(--muted)",
              marginTop: 6,
              display: "-webkit-box",
              WebkitLineClamp: 2,
              WebkitBoxOrient: "vertical",
              overflow: "hidden",
            }}
          >
            {book.description}
          </div>
        )}
        <div style={{ marginTop: "auto", paddingTop: 12, borderTop: "1px dashed var(--line)", fontSize: 12, color: "var(--ink-soft)" }}>
          <span className="vx-num" style={{ fontSize: 15 }}>
            {book.unitCount}
          </span>{" "}
          单元 ·{" "}
          <span className="vx-num" style={{ fontSize: 15 }}>
            {book.wordCount}
          </span>{" "}
          词
        </div>
      </div>
    </div>
  );
}

function CreateBookModal({ isAdmin, onClose, onCreated }: { isAdmin: boolean; onClose: () => void; onCreated: (id: string) => void }) {
  const [form] = Form.useForm<BookForm>();
  const { message } = useApp();
  const queryClient = useQueryClient();
  const create = useMutation({
    mutationFn: (v: BookForm) => api.post<{ id: string }>("/books", { name: v.name, description: v.description || null, isSystem: isAdmin ? !!v.isSystem : undefined }),
    onSuccess: (b) => {
      queryClient.invalidateQueries({ queryKey: ["books"] });
      message.success("词书已创建");
      onClose();
      onCreated(b.id);
    },
    onError: (e) => message.error(errorMessage(e)),
  });

  return (
    <Modal open title="新建词书" okText="创建" cancelText="取消" confirmLoading={create.isPending} onCancel={onClose} onOk={() => form.submit()}>
      <Form<BookForm> form={form} layout="vertical" onFinish={(v) => create.mutate(v)} initialValues={{ isSystem: false }}>
        <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true, message: "请填写词书名称" }, { max: 60, message: "名称过长" }]}>
          <Input placeholder="例如：八年级上册" maxLength={60} />
        </Form.Item>
        <Form.Item name="description" label="描述" rules={[{ max: 300, message: "描述过长" }]}>
          <Input.TextArea rows={3} placeholder="可选" maxLength={300} showCount />
        </Form.Item>
        {isAdmin && (
          <Form.Item name="isSystem" label="设为系统词书" valuePropName="checked" extra="系统词书对所有用户可见，只有管理员可以编辑。">
            <Switch />
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
}
