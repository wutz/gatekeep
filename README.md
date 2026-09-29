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

### 自定义命令分级

管理员可以在运行时调整任意命令的级别，不需要改 `policy.yaml` 也不需要重启。规则存进数据库，保存后立即生效。

匹配顺序：**shell 元字符 → 锁定的内置规则（`locked: true`）→ 自定义规则（按优先级，小的先）→ 其余内置规则 → `default_level`**。

- 一条规则的写法：程序 glob（支持 `{a,b}`）+ 参数须匹配的正则（可选）+ 参数不得匹配的正则（可选）+ 级别，可以限定只对某个目标生效。
- 升级：例如把 `kubectl logs -n prod …` 提到 L2，这样 Agent 读生产日志也要审批。
- 降级：例如把某台机上的 `systemctl restart kubelet` 降到 L0，Agent 就可以直接执行。
- **锁定的内置规则不能被覆盖**，包括 `rm -r`、`mkfs`、`dd`、删 ns/node/pool/fs、关机、读密钥文件。
- 不带参数条件的通配规则（`program: *`）不能低于 L2，防止一条规则把所有未知命令都放行给 Agent。
- 只有 admin 能增、改、删规则；其他人类可以查看。创建、修改（记录修改前后的值）、删除和越权尝试都会写入审计哈希链。
- 控制台里有「试算」：保存前输入样例命令，可以看到它们的级别会怎么变化。

```sh
# CLI（admin 令牌）
gk rules                                   # 列出
gk rules add --name prod-logs --program kubectl --args '^logs\s.*-n\s+prod\b' --level 2 --note "生产日志含用户数据"
gk rules add --name kubelet-restart-gpu105 --program systemctl --args '^restart\s+kubelet$' \
  --level 0 --target gpu-105 --priority 10
gk rules rm 3
gk -t gpu-105 check systemctl restart kubelet   # rule: custom:kubelet-restart-gpu105
```

API：`GET/POST /api/v1/rules`、`PUT/DELETE /api/v1/rules/{id}`、`POST /api/v1/rules/test`（试算）。

### 自定义命令

管理员可以把常用运维操作定义成**带参数的命令模板**，例如 `restart-service` 就是 `systemctl restart {{service}}`，其中 `service` 只允许取 `kubelet|containerd`。人类在控制台「自定义命令」页或用 `gk run` 执行；Agent 通过 MCP 的 `list_commands` / `run_command` 发现并调用。

- **不经过 shell**：模板先按 shell 规则拆成 argv，参数值只替换进单个 argv 元素，所以一个值不可能变成多个参数或管道。
- **参数校验**：每个参数的值必须完整匹配它的正则。没写正则时，默认只允许不以 `-` 开头的普通单词（字母、数字和 `_.:@/=+-`），防止注入选项。参数可以设默认值；可选参数留空时，它所在的整个 argv 元素会被去掉。
- **级别**：由命令自己声明。实际执行时仍会过一遍**锁定的内置规则和 shell 元字符检查**，只会被抬高，不会降低。例如声明为 L0 的 `rm -rf /tmp/{{dir}}` 实际按 L3 处理。
- **目标**：可以限定只能在某些目标上执行；不限定时可在全部目标上执行。
- **审批和审计**：每次执行都是一条普通请求，走同样的授权和审批流程。请求上会标出「自定义命令」名称，审批人能看到渲染后的完整命令。参数不合法的调用会记为 `command.invalid`；命令的增、改、删和越权尝试都会写入审计链。
- 只有 admin 能定义命令。其他人只能看到自己可用目标上、已启用的命令。

```sh
# 定义（admin）
gk cmd add --name restart-service --template 'systemctl restart {{service}}' \
  --param 'service=kubelet|containerd' --level 1 --target gpu-103 --target gpu-105 --target gpu-106
gk cmd add -f tail-journal.json      # 复杂定义（参数说明、默认值、可选）可用 JSON
gk cmd                               # 列出
gk cmd rm 2

# 使用
gk -t gpu-105 run restart-service service=kubelet --dry-run   # 只渲染和分级，不执行
gk -t gpu-105 -r "kubelet 卡死" --wait 600 run restart-service service=kubelet
```

API：`GET/POST /api/v1/commands`、`PUT/DELETE /api/v1/commands/{id}`、`POST /api/v1/commands/{name}/run`（body：`{target, params, reason, wait, dry_run}`）。

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

**MCP（推荐）** —— Agent 获得 `run` / `check` / `list_commands` / `run_command` / `list_targets` / `get_request` / `wait_request` / `cancel_request` 工具，连接时会收到使用说明：

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
- **CLI**：`gk run <自定义命令> k=v…`、`gk pending`、`gk approve <id> [备注]`、`gk reject <id> [备注]`、`gk history`、`gk audit`。

## 目录

```
cmd/gatekeepd     服务端（HTTP API、MCP、静态前端）
cmd/gk            客户端 CLI
internal/policy   命令分级与授权
internal/commands 自定义命令模板：校验与渲染
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
