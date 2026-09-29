import { Alert, Button, Input, Spin, Tag, LoadingOutlined, RollbackOutlined } from "@/ui";
import { AI_PROMPT_MAX_LENGTH, type AiPreviewWord } from "@vinx/shared";
import { runningLabel, type AiJobState } from "@/lib/useAiJob";

/**
 * 生成前预览（K41）：这次用的单词（可去掉个别词）+ 可编辑的可见提示词。
 * 输出格式由服务端自动加上，不在这里显示；修改只用于这一次生成。
 */
export function AiPromptPreview({
  words,
  onRemoveWord,
  prompt,
  onPromptChange,
  onRestore,
  dirty,
  loading,
  disabled,
  emptyText = "没有可用的单词",
}: {
  words: AiPreviewWord[];
  /** 不传时单词不能去掉（例如单个词补例句） */
  onRemoveWord?: (id: string) => void;
  prompt: string;
  onPromptChange: (text: string) => void;
  onRestore: () => void;
  /** 提示词是否手改过（决定「恢复」是否可点） */
  dirty: boolean;
  loading?: boolean;
  disabled?: boolean;
  emptyText?: string;
}) {
  const length = prompt.trim().length;
  const tooLong = length > AI_PROMPT_MAX_LENGTH;
  return (
    <div data-testid="ai-preview">
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8, color: "var(--ink-soft)", fontSize: 13 }}>
        <span>这次用的单词（{words.length}）</span>
        {loading && <Spin size="small" indicator={<LoadingOutlined spin />} />}
      </div>
      {words.length === 0 ? (
        <div style={{ color: "var(--muted)", fontSize: 13, marginBottom: 12 }}>{loading ? "正在准备…" : emptyText}</div>
      ) : (
        <div data-testid="ai-preview-words" style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 12 }}>
          {words.map((w) => (
            <Tag
              key={w.id}
              bordered={false}
              closable={Boolean(onRemoveWord) && !disabled}
              onClose={(e) => {
                e.preventDefault();
                onRemoveWord?.(w.id);
              }}
              closeIcon={onRemoveWord ? <span aria-label={`去掉 ${w.spelling}`}>×</span> : undefined}
              style={{ marginInlineEnd: 0, padding: "2px 8px", maxWidth: "100%", whiteSpace: "normal", background: "var(--primary-soft)", color: "var(--ink)" }}
            >
              <span className="vx-word" style={{ fontWeight: 600 }}>
                {w.spelling}
              </span>
              <span style={{ color: "var(--ink-soft)", marginLeft: 6, fontSize: 12 }}>{w.definition}</span>
            </Tag>
          ))}
        </div>
      )}

      <Input.TextArea
        aria-label="提示词"
        value={prompt}
        disabled={disabled}
        onChange={(e) => onPromptChange(e.target.value)}
        autoSize={{ minRows: 6, maxRows: 16 }}
        status={tooLong ? "error" : undefined}
        style={{ fontSize: 13 }}
      />
      <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 8, marginTop: 8 }}>
        <span style={{ color: "var(--muted)", fontSize: 12, lineHeight: 1.6 }}>
          可以按这次的需要修改，只对这一次生成生效；输出格式由系统自动加上。
          <span style={{ color: tooLong ? "var(--bad)" : "var(--muted)", marginLeft: 4, whiteSpace: "nowrap" }}>
            {length} / {AI_PROMPT_MAX_LENGTH}
          </span>
        </span>
        <Button size="small" icon={<RollbackOutlined />} onClick={onRestore} disabled={!dirty || disabled}>
          恢复
        </Button>
      </div>
    </div>
  );
}

/** 任务进度：生成中显示已用秒数，失败显示具体原因 */
export function AiJobProgress<T>({ state }: { state: AiJobState<T> }) {
  if (state.phase === "running") {
    return (
      <div role="status" style={{ color: "var(--ink-soft)", fontSize: 13, marginTop: 10, textAlign: "center" }}>
        <LoadingOutlined spin style={{ marginRight: 6 }} />
        {runningLabel(state.elapsedMs)}
      </div>
    );
  }
  if (state.phase === "failed") {
    return <Alert type="error" showIcon style={{ marginTop: 12 }} message="生成失败" description={<span style={{ wordBreak: "break-word" }}>{state.error}</span>} />;
  }
  return null;
}
