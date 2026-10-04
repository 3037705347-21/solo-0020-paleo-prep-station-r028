# 古生物修复制备台

古生物修复制备台是一个使用 Go 编写的后端基线，用于记录化石标本、审批修复方案、回填制备进度，并在全部工序完成后办理研究借阅放行。

该服务只使用进程内仓储，便于在本机重复启动和检查。它不依赖数据库、远端接口或其他在线资源。

## 目录

- `cmd/paleo-prep-station`：HTTP 服务入口和监听参数。
- `cmd/workflowcheck`：通过真实 HTTP 处理器执行四条业务链路的本地验证命令。
- `internal/httpapi`：路由、请求解码、状态码映射和 JSON 输出。
- `internal/station`：跨领域对象的编排、事务边界和业务流程。
- `internal/domain`：标本、方案、进度和放行记录的状态与不变量。
- `internal/repository`：互斥保护的内存仓储和原子提交。
- `internal/contracts`：稳定错误模型和公共请求结构。

## 构建与运行

```bash
go build ./...
go run ./cmd/paleo-prep-station --addr 127.0.0.1:18133
```

服务启动后，`GET /healthz` 返回就绪状态。

## 主要接口

- `GET /specimens` 与 `POST /specimens`
- `GET /specimens/{id}`
- `POST /specimens/{id}/plans`
- `GET /plans/{id}`
- `POST /plans/{id}/approval`
- `POST /plans/{id}/progress`
- `POST /specimens/{id}/release`
- `POST /specimens/{id}/release/revocation`

## 调用者身份与职责边界

所有写接口必须携带调用者身份请求头，身份在每次请求时独立校验，角色变更对下一个请求立即生效：

- `X-Actor-Name`：调用者姓名，会写入 `registered_by`、`created_by`、`approved_by`、`recorded_by` 等归属字段。
- `X-Actor-Role`：调用者角色，取值 `registrar`（登记员）、`prep_lead`（制备负责人）、`preparator`（制备人员）、`curator`（标本管理员）。

职责边界按动作校验，越权请求返回 `403` 且不改变任何业务状态：

- 登记标本：仅 `registrar`。
- 创建与审批方案：仅 `prep_lead`，且方案创建人不能审批自己创建的方案。
- 记录进度：仅 `preparator`，且方案审批人不能记录该方案的进度。
- 办理与撤销放行：仅 `curator`；撤销保留原放行记录并把标本置回 `stabilized`，同一记录不能重复撤销，标本也不能再次放行。

请求体中的 `approved_by`、`recorded_by` 等姓名字段继续兼容：留空时取调用者姓名，与调用者不一致时返回 `403`。

## 本地验证

每条业务链路都可以独立执行：

```bash
go run ./cmd/workflowcheck --workflow register-specimen
go run ./cmd/workflowcheck --workflow approve-prep-plan
go run ./cmd/workflowcheck --workflow log-preparation
go run ./cmd/workflowcheck --workflow release-for-study
go run ./cmd/workflowcheck --workflow revoke-release
```

这些检查会启动临时 HTTP 处理器并调用真实公共路由，同时覆盖越权拒绝、自审批拒绝和并发审批的确定结果。

## 环境变量

项目不需要环境变量，也没有外部服务依赖。

## 测试边界

本基线按要求暂不生成单元测试、测试夹具或端到端测试文件。后续任务阶段会补充红绿验证测试，届时再替换或扩展当前的生产级链路检查。
