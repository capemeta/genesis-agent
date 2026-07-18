# ProducedResource Artifact Delivery 剩余缺口 Implementation Plan

> **For agentic workers:** 按文档 `2026-07-17-produced-resource-artifact-delivery-architecture.md` 实施；禁止兼容旧链路；TDD。

**Goal:** 补齐 OutputReservation、durable 装配、产品控制面、错误码/path_map/文档对齐，使实现与架构文档一致。

**Architecture:** workspace 管资源身份与 Reader；artifact 管 Deliverable/Reservation/Publication/Delivery/Completion；skill Harness 通过 reservation 注入受控输出，唯一候选自动发布；products 只做装配。

**Tech Stack:** Go、现有 artifact/workspace/skill 能力域、CLI/Desktop/Enterprise bootstrap

---

## Tasks

### Task 1: OutputReservationService
- [x] Reserve + EnvBindings + Harness 接入（含 task_job 分离 OutputDir）

### Task 2: 错误码对齐文档 §13
- [x] PRODUCED_RESOURCE_* / DELIVERABLE_* 

### Task 3: Durable executor object 装配
- [x] ExecutorObjectResolver/Reader 装配
- [ ] leased→durable 提升（跨仓 residual）

### Task 4: Desktop / Enterprise
- [x] Desktop RunWorkspace 控制面；Enterprise BuildTenantDependencies

### Task 5: path_map / Gate / 文档
- [x] 移除模型可见 path_map/skill_dir/work_dir；GatePipeline；§16 文档

### Task 6: 验证
- [x] 相关包 go test 通过
