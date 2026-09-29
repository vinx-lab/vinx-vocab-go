import { useEffect, useState } from "preact/hooks";
import { Alert, ApiOutlined, Button, Checkbox, Form, Input, InputNumber, Popconfirm, Radio, RollbackOutlined, SaveOutlined, Space, Tag, useApp } from "@/ui";
import { useMutation, useQuery, useQueryClient } from "@/lib/query";
import {
  AI_SETTING_PROVIDER_LABEL,
  AI_SETTING_PROVIDERS,
  AI_TIMEOUT_SEC_MAX,
  AI_TIMEOUT_SEC_MIN,
  type AiSettingProvider,
  type AiSettingsInput,
  type AiSettingsView,
  type AiTestResult,
} from "@vinx/shared";
import { api, errorMessage } from "@/lib/api";
import { ErrorBlock, Loading, PageHeader } from "@/components/ui";
import { AiPromptsCard } from "./AiPromptsCard";
import { EditionCard } from "./EditionCard";

interface FormValues {
  provider: AiSettingProvider;
  baseUrl?: string;
  apiKey?: string;
  clearApiKey?: boolean;
  model?: string;
  timeoutSec?: number;
}

const toForm = (v: AiSettingsView): FormValues => ({
  provider: v.provider,
  baseUrl: v.baseUrl ?? "",
  apiKey: "",
  clearApiKey: false,
  model: v.model,
  timeoutSec: v.timeoutSec,
});

const toInput = (f: FormValues): AiSettingsInput => ({
  provider: f.provider,
  baseUrl: f.provider === "openai" ? f.baseUrl?.trim() || null : null,
  apiKey: f.apiKey?.trim() || undefined,
  clearApiKey: f.clearApiKey || undefined,
  model: f.model?.trim() || undefined,
  timeoutSec: f.timeoutSec ?? undefined,
});

/** 系统设置 → 版本（Go 版新增）；AI 接口（K38）：保存后立即生效，页面配置优先于环境变量；下面是 AI 提示词（K40） */
export function SettingsPage() {
  const { message } = useApp();
  const qc = useQueryClient();
  const [form] = Form.useForm<FormValues>();
  const provider = Form.useWatch("provider", form);
  const [testResult, setTestResult] = useState<AiTestResult | null>(null);

  const query = useQuery({ queryKey: ["settings", "ai"], queryFn: () => api.get<AiSettingsView>("/settings/ai") });
  const view = query.data;

  useEffect(() => {
    if (view) form.setFieldsValue(toForm(view));
  }, [view, form]);

  const applied = (data: AiSettingsView, text: string) => {
    message.success(text);
    setTestResult(null);
    qc.setQueryData(["settings", "ai"], data);
    form.setFieldsValue(toForm(data));
    qc.invalidateQueries({ queryKey: ["config"] });
    qc.invalidateQueries({ queryKey: ["ai", "status"] });
  };

  const save = useMutation({
    mutationFn: (values: FormValues) => api.put<AiSettingsView>("/settings/ai", toInput(values)),
    onSuccess: (data) => applied(data, "已保存，立即生效"),
    onError: (e) => message.error(errorMessage(e)),
  });

  const reset = useMutation({
    mutationFn: () => api.del<AiSettingsView>("/settings/ai"),
    onSuccess: (data) => applied(data, "已恢复为环境变量的配置"),
    onError: (e) => message.error(errorMessage(e)),
  });

  const test = useMutation({
    mutationFn: (values: FormValues) => api.post<AiTestResult>("/settings/ai/test", toInput(values)),
    onMutate: () => setTestResult(null),
    onSuccess: (r) => setTestResult(r),
    onError: (e) => setTestResult({ ok: false, ms: 0, error: errorMessage(e) }),
  });

  const runTest = async () => test.mutate(await form.validateFields());

  // 与旧版一致：AI 设置还没取到 / 取失败时整页显示加载 / 错误（不先渲染页头和其他卡片）
  if (query.isLoading) return <Loading />;
  if (query.isError || !view) return <ErrorBlock error={query.error} onRetry={() => query.refetch()} />;

  return (
    <div className="vx-page">
      <PageHeader eyebrow="Settings" title="系统设置">
        版本、AI 接口和 AI 提示词，保存后立即生效，不需要重启服务。
      </PageHeader>

      <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
        <EditionCard />

        <div className="vx-card" style={{ padding: 20, maxWidth: 640 }}>
          <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8, marginBottom: 12 }}>
            <h2 className="vx-title" style={{ fontSize: 18, margin: 0 }}>
              AI 接口
            </h2>
            <Tag color={view.enabled ? "green" : "default"} bordered={false}>
              {view.enabled ? "可用" : "未启用"}
            </Tag>
          </div>
          <div style={{ color: "var(--muted)", fontSize: 13, marginBottom: 16 }}>
            用于词书页「AI 补例句」和「短文巩固」。
            {view.source === "db" ? `当前生效：页面保存的配置${view.updatedAt ? `（${new Date(view.updatedAt).toLocaleString("zh-CN")}）` : ""}。` : "当前生效：环境变量（AI_*）。"}
          </div>

          {view.keyUndecryptable && (
            <Alert type="warning" showIcon style={{ marginBottom: 16 }} message="已保存的 Key 无法解密，请重新填写" description="加密密钥（SETTINGS_SECRET）可能已更换，AI 暂按未配置 Key 处理。" />
          )}

          <Form form={form} layout="vertical" initialValues={toForm(view)} onFinish={(v) => save.mutate(v)} onValuesChange={() => setTestResult(null)}>
            <Form.Item name="provider" label="供应商">
              <Radio.Group optionType="button" options={AI_SETTING_PROVIDERS.map((p) => ({ value: p, label: AI_SETTING_PROVIDER_LABEL[p] }))} />
            </Form.Item>

            {provider === "openai" && (
              <Form.Item
                name="baseUrl"
                label="接口地址"
                extra="任何 OpenAI 兼容接口：本机 Ollama、vLLM、LM Studio，或 DeepSeek、通义、智谱等云服务。"
                rules={[
                  { required: true, message: "请填写接口地址" },
                  { pattern: /^https?:\/\/\S+$/i, message: "以 http:// 或 https:// 开头" },
                ]}
              >
                <Input placeholder="http://localhost:11434/v1" autoComplete="off" />
              </Form.Item>
            )}

            {provider !== "off" && (
              <>
                <Form.Item name="apiKey" label="API Key" extra={provider === "openai" ? "本机服务通常不需要 Key。" : undefined}>
                  <Input.Password placeholder={view.apiKey.set ? `已设置${view.apiKey.last4 ? `，末 4 位 ${view.apiKey.last4}` : ""}（留空沿用）` : "未设置"} autoComplete="new-password" />
                </Form.Item>
                {view.apiKey.set && (
                  <Form.Item name="clearApiKey" valuePropName="checked" style={{ marginTop: -12 }}>
                    <Checkbox>清除已保存的 Key</Checkbox>
                  </Form.Item>
                )}
                <Form.Item name="model" label="模型" rules={[{ required: true, whitespace: true, message: "请填写模型" }]}>
                  <Input placeholder={provider === "anthropic" ? "claude-sonnet-5" : "qwen2.5:7b-instruct"} autoComplete="off" />
                </Form.Item>
                <Form.Item name="timeoutSec" label="超时（秒）">
                  <InputNumber min={AI_TIMEOUT_SEC_MIN} max={AI_TIMEOUT_SEC_MAX} precision={0} style={{ width: 160 }} />
                </Form.Item>
              </>
            )}

            {testResult && (
              <Alert
                type={testResult.ok ? "success" : "error"}
                showIcon
                style={{ marginBottom: 16 }}
                message={testResult.ok ? `连接成功，耗时 ${testResult.ms} ms` : "连接失败"}
                description={testResult.ok ? undefined : testResult.error}
              />
            )}

            <Space wrap>
              <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={save.isPending}>
                保存
              </Button>
              <Button icon={<ApiOutlined />} onClick={runTest} loading={test.isPending} disabled={provider === "off"}>
                测试连接
              </Button>
              <Popconfirm
                title="恢复为环境变量？"
                description="会删除页面保存的配置（包括 Key），改用 .env 里的 AI_* 设置。"
                okText="恢复"
                cancelText="取消"
                onConfirm={() => reset.mutate()}
                disabled={view.source === "env"}
              >
                <Button icon={<RollbackOutlined />} loading={reset.isPending} disabled={view.source === "env"}>
                  恢复为环境变量
                </Button>
              </Popconfirm>
            </Space>
            <div style={{ color: "var(--muted)", fontSize: 12, marginTop: 12 }}>测试连接使用上面填写的值，不会保存。API Key 加密存储，页面上只显示末 4 位。</div>
          </Form>
        </div>

        <AiPromptsCard />
      </div>
    </div>
  );
}
