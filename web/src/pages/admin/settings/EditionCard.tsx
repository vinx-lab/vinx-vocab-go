import { Button, Tag, useApp } from "@/ui";
import { useMutation, useQueryClient } from "@/lib/query";
import { api, errorMessage } from "@/lib/api";
import { EDITION_LABEL, type Edition } from "@vinx/shared";
import { useConfig } from "@/lib/useConfig";

const EFFECT_TEXT: Record<Edition, string> = {
  personal: "切换为个人版会隐藏班级、邀请码、布置计划、用户管理入口，并关闭注册；已有数据不会删除，已有账号仍可登录，但只能看到自己的内容。随时可以切换回来。",
  school: "切换为班级版会恢复班级、邀请码、布置计划、用户管理入口，并按班级版规则开放注册（由 SIGNUP_ENABLED 控制：环境变量或数据目录的 config.toml，设为 false 即关闭）；此前的数据原样恢复。",
};

/** 系统设置 → 版本：管理员双向切换个人版 / 班级版；锁定时只读 */
export function EditionCard() {
  const { message, modal } = useApp();
  const qc = useQueryClient();
  const config = useConfig();

  const switchEdition = useMutation({
    mutationFn: (edition: Edition) => api.put("/settings/edition", { edition }),
    onSuccess: async (_, edition) => {
      message.success(`已切换为${EDITION_LABEL[edition]}`);
      await qc.invalidateQueries({ queryKey: ["config"] });
    },
    onError: (e) => message.error(errorMessage(e, "切换失败")),
  });

  const target: Edition = config.edition === "school" ? "personal" : "school";

  const confirmSwitch = () => {
    modal.confirm({
      title: `切换为${EDITION_LABEL[target]}？`,
      content: EFFECT_TEXT[target],
      okText: "确定切换",
      cancelText: "取消",
      onOk: () => switchEdition.mutateAsync(target),
    });
  };

  return (
    <div className="vx-card" style={{ padding: 20, maxWidth: 640 }}>
      <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8, marginBottom: 12 }}>
        <h2 className="vx-title" style={{ fontSize: 18, margin: 0 }}>
          版本
        </h2>
        <Tag color={config.edition === "school" ? "blue" : "default"} bordered={false}>
          当前：{EDITION_LABEL[config.edition]}
        </Tag>
      </div>

      {config.editionLocked ? (
        <div style={{ color: "var(--muted)", fontSize: 13 }}>版本由启动配置指定，不能在界面上切换（环境变量 VINX_EDITION，或数据目录 config.toml 里的 VINX_EDITION）。</div>
      ) : (
        <>
          <div style={{ color: "var(--muted)", fontSize: 13, marginBottom: 16 }}>
            {config.edition === "school" ? "班级版：包含班级、批量建号、给学生安排计划、学生记录等教学功能。" : "个人版：单人使用，没有班级、没有他人安排、没有用户管理。"}
          </div>
          <Button loading={switchEdition.isPending} onClick={confirmSwitch}>
            切换为{EDITION_LABEL[target]}
          </Button>
        </>
      )}
    </div>
  );
}
