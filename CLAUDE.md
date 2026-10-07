# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

@AGENTS.md

## 工作入口

先读 [README.md](README.md)，再按任务阅读：

- [产品说明](docs/product.md)
- [功能与使用流程](docs/features.md)（面向推广；产品功能变化时同步更新）
- [架构与约定](docs/architecture.md) 与 [架构决策记录](docs/architecture-decisions/README.md)
- [开发规则](docs/development.md) 与 [产品决定](docs/decisions.md)
- [运行、部署与验证](docs/deployment.md)
- [需求说明](docs/specs/README.md)（进行中和已完成的较大改动）
- [契约测试](contract/README.md)、[E2E](e2e/README.md)

## 常用命令（根目录）

```bash
make test            # Go 单测
make vet
make build           # 构建前端 + 嵌入 + dist/vinx-vocab
make cross           # Windows exe + Linux 交叉编译
make e2e             # Playwright（自动起两个实例）
./dist/vinx-vocab seed-demo --data <目录>   # 演示数据：admin/teacher/student/student2@vinx.test，密码 dev123456，邀请码 DEMO01
```

本机相关的事实和个人约定如果存在，写在不提交的 `CLAUDE.local.md` 里。
