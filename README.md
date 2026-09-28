# gatekeep

面向 **Agent 与人类** 的生产环境访问控制网关。所有对 Linux / Kubernetes / Ceph / GPFS 的命令都经由 gatekeep 执行：自动分级、按身份授权、变更需人类审批、全程写入防篡改审计日志。

```
Agent (Claude Code / Codex / …) ──MCP──┐
Agent / 脚本 ──────────── gk CLI ──────┼──▶ gatekeepd ──ssh──▶ 10.243.144.51-53 (k8s 管理 + GPFS accessing)
人类 ─────────── Web 控制台 / gk ──────┘   │  策略分级         10.243.145.103,105,106 (k8s GPU worker + GPFS owning)
                                           │  审批队列
                                           └─ SQLite：请求 + 哈希链审计
```

## 核心规则

| 级别 | 含义 | 例子 |
|---|---|---|
| **L0 只读** | 状态、日志、查询 | `kubectl get/describe/logs`、`journalctl`、`ceph -s`、`mmlscluster`、`df` |
| **L1 低风险** | 可逆的小变更 | `systemctl restart`、`kubectl delete pod`、`kubectl cordon`、`ceph osd set noout`、`mmmount` |
| **L2 高风险** | 实质变更（**未匹配任何规则的命令也归入此级**） | `kubectl apply/scale/exec`、`ceph osd out`、`mmchconfig`、读取 secret/密钥文件 |
| **L3 危险** | 破坏性 / 数据丢失 | `kubectl delete ns/node`、`ceph osd pool delete`、`mmdelfs`、`rm -r`、带 `;|&$` 等 shell 元字符 |

| 身份 | 可直接执行 | 需审批 | 可审批 |
|---|---|---|---|
| **agent** | L0 | L1–L3（必须填写理由） | — |
| **viewer**（人） | L0 | 不可提交变更 | — |
| **operator**（人） | L0–L1 | L2–L3 | ≤ L2 |
| **admin**（人） | L0–L2 | L3 | 全部 |

- 任何人都 **不能审批自己的请求**；Agent 永远不能审批。
- L3 在控制台审批时需手动输入目标名二次确认。
- 待审批请求默认 60 分钟过期。
- 命令以 argv 直接执行，**不经过 shell**；通过 SSH 时每个参数都单独转义。

策略在 [`configs/policy.yaml`](configs/policy.yaml)，按顺序首条匹配生效，可按需增删。

## 审计

每个动作（提交、审批、驳回、执行、结束、越权尝试、认证失败、服务启停）都追加一条记录，记录中包含上一条记录的 SHA-256，形成哈希链；中间任何修改或删除都会被检测出来：

```sh
gk audit --verify                         # 通过 API
bin/gatekeepd verify-audit -db gatekeep.db # 离线校验
```

## 快速开始

```sh
make web build                  # 构建前端 (TanStack Start SPA) 与后端 (Go)
cp configs/gatekeep.example.yaml configs/gatekeep.yaml
bin/gatekeepd gen-token         # 为每个身份生成令牌，配置里只放 token_sha256
make dev                        # 打开 http://127.0.0.1:8740 ，用人类令牌登录
```

目标主机通过 gatekeepd 所在机器的 `ssh`（BatchMode）连接，请为运行 gatekeepd 的用户配置好到各节点的密钥。

## Agent 接入

**MCP（推荐）** —— Agent 获得 `run` / `check` / `list_targets` / `get_request` / `wait_request` / `cancel_request` 工具，连接时会收到使用说明：

```sh
claude mcp add --transport http gatekeep http://gatekeep:8740/mcp \
  --header "Authorization: Bearer $GATEKEEP_TOKEN"
```

**gk CLI** —— 在命令前加 `gk`，输出与退出码与原命令一致：

```sh
export GATEKEEP_URL=http://gatekeep:8740 GATEKEEP_TOKEN=gk_... GATEKEEP_TARGET=k8s-mgmt-1
gk kubectl get nodes -o wide
gk -t gpu-105 journalctl -u kubelet -n 200
gk -r "kubelet 卡死需重启" --wait 600 systemctl restart kubelet   # 阻塞等待审批
```

需要审批时 `gk` 以退出码 75 返回并打印请求 id；被驳回/拒绝时退出码 77。

## 人类使用

- **Web 控制台**：审批队列（实时推送）、操作记录与输出、直接执行命令（输入时实时显示风险级别）、审计日志与完整性校验、接入说明。
- **CLI**：`gk pending`、`gk approve <id> [备注]`、`gk reject <id> [备注]`、`gk history`、`gk audit`。

## 目录

```
cmd/gatekeepd     服务端（HTTP API、MCP、静态前端）
cmd/gk            客户端 CLI
internal/policy   命令分级与授权
internal/store    SQLite：请求状态机 + 哈希链审计
internal/executor 本地 / SSH 执行，输出截断与超时
internal/server   服务层、REST、MCP、SSE
web/              TanStack Start 控制台（遵循 DESIGN.md，主题色 teal #0d9488）
configs/          示例配置与默认策略
deploy/           systemd unit
```

## 开发

```sh
make test     # Go 单测 + 前端类型检查
make smoke    # 启动临时实例跑端到端流程
cd web && npm run dev   # 前端热更新，API 代理到 127.0.0.1:8740
```

## 后续可做

- OIDC / LDAP 登录替代静态令牌；审批通知（飞书 / 邮件 / Webhook）
- 审计日志外送（syslog / S3 WORM）并定期锚定链头哈希
- 按目标标签的细粒度策略（如 GPU 节点更严格）；双人审批 L3
- 原生 K8s API / Ceph REST 执行器，免 SSH
