import { useEffect, useState } from "preact/hooks";
import { Alert, Button, Collapse, Input, Popconfirm, RollbackOutlined, SaveOutlined, Space, Tabs, Tag, useApp } from "@/ui";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import {
  AI_PROMPT_KEYS,
  AI_PROMPT_LABEL,
  AI_PROMPT_MAX_LENGTH,
  type AiPromptItem,
  type AiPromptKey,
  type AiPromptsView,
} from "@vinx/shared";
import { api, errorMessage } from "@/lib/api";
import { ErrorBlock, Loading } from "@/components/ui";

const QUERY_KEY = ["settings", "ai", "prompts"];

function PromptEditor({ item }: { item: AiPromptItem }) {
  const { message } = useApp();
  const qc = useQueryClient();
  const [text, setText] = useState(item.current);
  const [error, setError] = useState<string | null>(null);

  // 保存 / 恢复后以服务端返回的内容为准
  useEffect(() => {
    setText(item.current);
    setError(null);
  }, [item.current]);

  const applied = (data: AiPromptsView, tip: string) => {
    message.success(tip);
    setError(null);
    qc.setQueryData(QUERY_KEY, data);
  };

  const save = useMutation({
    mutationFn: () => api.put<AiPromptsView>("/settings/ai/prompts", { [item.key]: text }),
    onSuccess: (data) => applied(data, "已保存，下一次生成立即使用"),
    onError: (e) => setError(errorMessage(e)),
  });

  const reset = useMutation({
    mutationFn: () => api.del<AiPromptsView>(`/settings/ai/prompts/${item.key}`),
    onSuccess: (data) => applied(data, "已恢复默认"),
    onError: (e) => message.error(errorMessage(e)),
  });

  const dirty = text !== item.current;
  const label = AI_PROMPT_LABEL[item.key];

  return (
    <div>
      <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8, marginBottom: 8 }}>
        <Tag data-testid={`prompt-status-${item.key}`} color={item.isDefault ? "default" : "orange"} bordered={false}>
          {item.isDefault ? "默认" : "已修改"}
        </Tag>
        {dirty && <span style={{ color: "var(--accent)", fontSize: 12 }}>有未保存的修改</span>}
      </div>

      <Input.TextArea
        aria-label={`${label}要求模板`}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          if (error) setError(null);
        }}
        autoSize={{ minRows: 8, maxRows: 20 }}
        count={{ show: true, max: AI_PROMPT_MAX_LENGTH, strategy: (v) => v.trim().length }}
        style={{ fontFamily: "var(--mono)", fontSize: 13 }}
      />

      <div style={{ color: "var(--muted)", fontSize: 12, margin: "10px 0 12px", lineHeight: 1.7 }}>
        输出格式由系统自动加上，这里只写要求。生成时会在后面接上这次的单词列表{item.key === "passage" ? "和主题" : ""}。
      </div>

      {error && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          message="没有保存"
          description={error}
        />
      )}

      <Space wrap>
        <Button type="primary" icon={<SaveOutlined />} loading={save.isPending} onClick={() => save.mutate()} disabled={!text.trim()}>
          保存
        </Button>
        <Popconfirm
          title="恢复默认？"
          description="会删除这一项的自定义内容，立即改用默认模板。"
          okText="恢复"
          cancelText="取消"
          onConfirm={() => reset.mutate()}
          disabled={item.isDefault}
        >
          <Button icon={<RollbackOutlined />} loading={reset.isPending} disabled={item.isDefault}>
            恢复默认
          </Button>
        </Popconfirm>
      </Space>

      <Collapse
        ghost
        size="small"
        style={{ marginTop: 12 }}
        items={[
          {
            key: "default",
            label: "查看默认内容",
            children: (
              <pre
                data-testid={`prompt-default-${item.key}`}
                style={{
                  margin: 0,
                  padding: 12,
                  whiteSpace: "pre-wrap",
                  wordBreak: "break-word",
                  fontFamily: "var(--mono)",
                  fontSize: 12,
                  color: "var(--ink-soft)",
                  background: "var(--paper-deep)",
                  border: "1px solid var(--line)",
                  borderRadius: 8,
                }}
              >
                {item.defaultText}
              </pre>
            ),
          },
        ]}
      />
    </div>
  );
}

/**
 * 系统设置 → AI 提示词（K41）：只编辑例句、短文两段默认要求模板。
 * 输出格式由系统自动加上，不在这里显示、也不检查；生成的人在生成前还可以按次修改。
 */
export function AiPromptsCard() {
  const query = useQuery({ queryKey: QUERY_KEY, queryFn: () => api.get<AiPromptsView>("/settings/ai/prompts") });
  const view = query.data;

  return (
    <div className="vx-card" style={{ padding: 20, maxWidth: 640 }}>
      <h2 className="vx-title" style={{ fontSize: 18, margin: "0 0 8px" }}>
        AI 提示词
      </h2>
      <div style={{ color: "var(--muted)", fontSize: 13, marginBottom: 8 }}>
        AI 生成例句和巩固短文时的默认要求（风格、长度、难度等）。保存后，生成前预览里的提示词立即按新模板拼出；
        生成的人还可以针对某一次再改。输出格式由系统自动加上。
      </div>
      {query.isLoading ? (
        <Loading />
      ) : query.isError || !view ? (
        <ErrorBlock error={query.error} onRetry={() => query.refetch()} />
      ) : (
        <Tabs items={AI_PROMPT_KEYS.map((k: AiPromptKey) => ({ key: k, label: AI_PROMPT_LABEL[k], children: <PromptEditor item={view[k]} /> }))} />
      )}
    </div>
  );
}
