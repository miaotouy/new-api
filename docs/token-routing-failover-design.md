# 密钥级渠道容灾与价格路由设计

## 1. 目标

在现有 `Token -> 分组 -> 渠道 priority/weight -> 全局重试` 之上增加一层密钥级路由策略，保持旧 Key 的行为完全兼容。

目标能力：

- 指定 Key 的最大可接受分组倍率；
- 指定分组或固定渠道作为候选；
- 手动设置回退顺序；
- 自动按价格、优先级和渠道状态选择；
- 指定分组也可以跨组故障转移；
- Key 级请求速率限制；
- 记录实际路由链、切换原因和最终计费分组。

旧 Key 未配置新字段时继续使用当前 `priority + weight + auto/cross_group_retry` 逻辑。

## 2. 密钥配置与接口

### 2.1 Token 字段

扩展 `Token`：

```json
{
  "route_mode": "auto",
  "auto_route_strategy": "priority",
  "max_ratio": 0,
  "failover_enabled": false,
  "rate_limit": 0,
  "rate_limit_window_seconds": 60
}
```

字段语义：

- `route_mode`: `auto` 或 `manual`；
- `auto_route_strategy`: `priority` 或 `price`；
- `max_ratio`: `0` 表示不限；否则只允许实际收费倍率不超过该值的候选分组；
- `failover_enabled`: 允许当前候选失败后继续尝试后续候选；
- `rate_limit`: 时间窗口内最大请求数，`0` 表示不限；
- `rate_limit_window_seconds`: 1 到 86400 秒。

保留现有 `cross_group_retry` 字段用于旧客户端兼容：

- 旧 Key 的 `cross_group_retry=true` 且分组为 `auto` 时，自动映射为新的故障转移策略；
- 新接口返回新字段，同时继续返回旧字段；
- 不删除或重命名现有数据库字段。

### 2.2 手动路由规则

新增 `TokenRouteRule` 表：

```text
id
token_id
position
kind          // group 或 channel
group_name
channel_id
enabled
```

使用 `(token_id, position)` 唯一索引。渠道规则不保存渠道密钥，只保存渠道 ID。

提供接口：

- `GET /api/token/:id/routes`
- `PUT /api/token/:id/routes`

所有写入必须验证 Token 所属用户、分组权限、渠道存在性和排序位置。删除 Token 时同步删除路由规则。

## 3. 路由计划服务

新增路由计划服务，统一生成当前请求可用候选：

```go
type RouteCandidate struct {
    Group         string
    ChannelID     int
    Priority      int64
    Weight        uint
    GroupRatio    float64
    EstimatedCost float64
}

type RoutePlan struct {
    Candidates []RouteCandidate
    Mode       string
    Strategy   string
    MaxRatio   float64
}
```

候选生成流程：

1. 校验用户可访问分组；
2. 根据 Token 分组确定起始范围：
   - `auto` 使用现有用户自动分组；
   - 指定分组先尝试指定分组，故障转移时再扩展到用户可用分组；
3. 应用 `max_ratio`；
4. 按请求模型、请求路径、渠道启用状态和 Ability 过滤；
5. 排除当前请求已经尝试过的渠道；
6. 生成最终候选链。

### 3.1 手动模式

- 按 `TokenRouteRule.position` 逐项尝试；
- `group` 规则内部继续使用现有 priority/weight；
- `channel` 规则只使用指定渠道；
- 无有效规则时返回配置错误，不静默绕过 Key 的限制。

### 3.2 自动模式

- `priority` 策略保持现有优先级和权重行为；
- `price` 策略按实际用户组倍率排序，再按 priority、响应时间、weight、channel ID 稳定排序。

当前仓库没有独立的渠道价格字段，因此 v1 的“价格路由”使用：

```text
实际用户组倍率 = service.GetUserGroupRatio(userGroup, candidateGroup)
预计成本 = 模型价格或模型倍率 × 实际用户组倍率
```

同一模型在不同渠道没有独立价格时，渠道之间按优先级、响应时间和权重处理。未来若增加渠道级价格，只需替换候选成本计算器。

## 4. 重试与故障转移

将现有 `RetryParam` 扩展为携带 `RoutePlan` 和已尝试候选集合，避免同一次请求重复命中相同渠道。

故障转移只在以下情况发生：

- `types.IsChannelError`；
- 允许重试的网络错误、上游 5xx 或配置的可重试状态码；
- 渠道自动禁用、额度耗尽、认证失效等不可恢复渠道错误。

以下情况不切换：

- `ErrOptionWithSkipRetry`；
- 客户端请求错误；
- 内容策略错误；
- 管理员显式指定 `specific_channel_id`；
- 没有剩余重试预算。

保留 `common.RetryTimes` 作为单请求总尝试上限，不新增 Key 级重试次数，避免和现有全局配置产生冲突。

每次切换写入：

```text
route_mode
candidate_group
candidate_channel_id
fallback_reason
attempt_index
max_ratio
```

详细信息放入现有日志 `other.admin_info`，普通用户日志不显示渠道内部信息。

## 5. 计费与额度安全

路由选择必须在首次预扣费前完成，最终选中的分组同步到 `RelayInfo`。

为了避免从低倍率渠道切换到高倍率渠道后出现响应已返回、额度无法补扣的问题：

- 在首次上游调用前，基于当前请求估算和所有合规候选分组计算保守的最大预扣额度；
- 使用现有 `ModelPriceHelper` 的计费规则和 `common.QuotaFromFloatChecked`、`common.QuotaFromDecimalChecked`；
- 请求成功后按最终实际分组和实际用量结算，多余预扣自动退还；
- 没有满足 `max_ratio` 的候选时，不进行任何预扣并返回无可用渠道；
- 所有倍率、窗口、排序权重和候选数量做有限值校验，拒绝 NaN、Inf、负值和超限输入；
- 涉及 tiered billing 时先遵循 `pkg/billingexpr/expr.md` 的预扣和结算规则。

必须覆盖“低价候选失败、高价候选成功”“预扣额度不足”“全部候选失败”和“结算低于预扣”四类路径。

## 6. Key 级速率限制

在 Token 认证完成后、渠道分配前增加 Token 级限流中间件：

- Redis 使用原子窗口计数；
- Redis 不可用时复用现有内存限流后备；
- 限流键使用 Token ID，不使用明文 Key；
- 超限返回 HTTP 429；
- 流式请求在进入 Relay 前计数一次；
- 限流失败的请求不进入渠道选择，也不产生计费记录。

## 7. 管理端界面

扩展 `web/default/src/features/keys`：

- 新增自动/手动路由模式；
- 自动模式下选择“现有优先级”或“价格优先”；
- 最大倍率输入，`0` 显示为不限；
- 故障转移开关；
- 请求数和时间窗口输入；
- 手动模式下打开路由编辑器：添加分组或渠道、拖拽调整顺序、显示渠道状态、分组倍率和模型支持情况；
- 自动移除或标记已禁用、已删除渠道；
- 所有新文案同步 `en/zh/fr/ja/ru/vi` 六种语言；
- 使用 React Hook Form + Zod 做前端校验，后端重复校验；
- UI 默认值映射到兼容的 `auto + priority + 无故障转移`。

## 8. 测试计划

### 8.1 后端

- Token 新字段创建、更新、读取和旧字段兼容；
- SQLite、MySQL、PostgreSQL 的 TokenRouteRule 迁移；
- 用户只能修改自己的路由规则；
- 手动分组顺序和固定渠道顺序；
- 自动 priority 模式与现有选择结果一致；
- 自动 price 模式按倍率排序，倍率相同时按 priority/响应时间/weight 稳定排序；
- `max_ratio=0`、边界倍率、负数、NaN、Inf 和超限值；
- 指定分组启用故障转移；
- `cross_group_retry` 旧行为不回归；
- 已尝试渠道不重复；
- 4xx、skip-retry、5xx、认证失效和自动禁用错误的切换矩阵；
- 预扣上限、最终结算、退款和额度不足；
- Token 级 Redis/内存限流及 HTTP 429；
- 日志包含回退链和原因，普通用户视图不泄漏 `admin_info`。

### 8.2 前端

- Zod schema 默认值和边界校验；
- 模式切换时表单字段归一化；
- 手动规则增删、拖拽排序和提交 payload；
- 已删除渠道的展示与提交保护；
- 速率限制与最大倍率输入；
- i18n key 完整性；
- `bun run typecheck`、相关 lint、生产构建。

## 9. 兼容性与默认值

- 新字段默认保持旧行为，故障转移默认关闭；
- 旧 `cross_group_retry` 保留并继续返回；
- `max_ratio=0` 表示不限；
- 手动路由为空时不自动绕过配置；
- 全局 `common.RetryTimes` 继续作为单请求总重试预算；
- 数据库迁移使用 GORM，兼容 SQLite、MySQL >= 5.7.8 和 PostgreSQL >= 9.6；
- 不修改项目受保护的 `new-api` 与 `QuantumNous` 标识。
