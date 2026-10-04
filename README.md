# 古生物修复制备台

古生物修复制备台是一个使用 Go 编写的后端基线，用于记录化石标本、审批修复方案、回填制备进度，并在全部工序完成后办理研究借阅放行。

该服务只使用进程内仓储，便于在本机重复启动和检查。它不依赖数据库、远端接口或其他在线资源。

## 目录

- `cmd/paleo-prep-station`：HTTP 服务入口和监听参数。
- `cmd/workflowcheck`：通过真实 HTTP 处理器执行四条业务链路的本地验证命令。
- `internal/httpapi`：路由、请求解码、状态码映射和 JSON 输出。
- `internal/station`：跨领域对象的编排、职责与归属校验、事务边界和业务流程。
- `internal/domain`：标本、方案、进度、放行记录和角色职责的状态与不变量。
- `internal/repository`：互斥保护的内存仓储、职责目录和原子提交。
- `internal/contracts`：稳定错误模型和公共请求结构。

## 构建与运行

```bash
go build ./...
go run ./cmd/paleo-prep-station --addr 127.0.0.1:18133
```

服务启动后，`GET /healthz` 返回就绪状态。

## 职责与操作人

所有写入口都会依据请求中的操作人姓名核对职责目录，未授权请求返回 `403` 且不改变业务状态。内置角色：

- `registrar`（登记员）：`POST /specimens`，身份字段 `registered_by`。
- `prep_lead`（制备负责人）：`POST /specimens/{id}/plans`（`created_by`）、`POST /plans/{id}/approval` 与 `POST /plans/{id}/cancellation`。方案创建人不能审批自己的方案，只有创建人本人可以撤销草稿方案。
- `preparator`（制备人员）：`POST /plans/{id}/progress`（`recorded_by`）。
- `curator`（标本管理员）：`POST /specimens/{id}/release`（`approved_by`）。记录过该标本进度的人不能批准放行。

服务内置一份默认名册（`registrar-song`、`lead-wang`、`lead-zhao`、`preparator-chen`、`keeper-zhou`，分别对应上述角色，其中两位负责人便于演示创建与审批分离）。启动时可用 `--operator name:role` 重复登记或变更职责，例如：

```bash
go run ./cmd/paleo-prep-station --operator temp-lead:prep_lead
```

运行中也可以通过 `station.Service.AssignRole` / `RevokeRole` 调整角色；角色读写与业务写入共用同一把仓储互斥锁，角色变更与并发决定按互斥顺序生效。

## 主要接口

- `GET /specimens` 与 `POST /specimens`
- `GET /specimens/{id}`
- `POST /specimens/{id}/plans`
- `GET /plans/{id}`
- `POST /plans/{id}/approval`
- `POST /plans/{id}/cancellation`
- `POST /plans/{id}/progress`
- `POST /specimens/{id}/release`

## 本地验证

每条业务链路都可以独立执行：

```bash
go run ./cmd/workflowcheck --workflow register-specimen
go run ./cmd/workflowcheck --workflow approve-prep-plan
go run ./cmd/workflowcheck --workflow log-preparation
go run ./cmd/workflowcheck --workflow release-for-study
```

这些检查会启动临时 HTTP 处理器并调用真实公共路由。

## 环境变量

项目不需要环境变量，也没有外部服务依赖。

## 测试边界

本基线按要求暂不生成单元测试、测试夹具或端到端测试文件。后续任务阶段会补充红绿验证测试，届时再替换或扩展当前的生产级链路检查。
