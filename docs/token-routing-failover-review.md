# 密钥级容灾路由代码审查报告

审查对象：`d252f76c3 feat(token-routing): 增强密钥路由：成本感知、加权选择与故障转移审计`

审查基线：`mmfork @ 5dca2f7d4`

配套设计文档：[`token-routing-failover-design.md`](./token-routing-failover-design.md)

## 0. 结论摘要

改动的主链路是正确的：路由计划、候选去重、故障审计、最大预扣费四条逻辑都能跑通，`go build ./...` 通过，`service` 包 4 个路由测试全绿。

发现 3 个需要处理的问题和 3 个建议项，没有发现计费安全（负额度／溢出）方面的缺陷。

| 编号 | 位置 | 级别 | 问题 |
|---|---|---|---|
| P1 | `middleware/rate-limit.go:265,295` | 中 | 升级瞬间 Redis 限流键类型冲突（LIST vs STRING），限流降级为单机内存 |
| P2 | `service/token_routing.go:438` | 中 | `sort.Strings(groups[1:])` 把管理员配置的容灾优先级抹平成字典序 |
| P3 | `controller/relay.go:264-288` | 中 | 按最贵候选分组预扣费，可能让本应命中廉价分组的请求被拒 |
| S1 | `middleware/rate-limit.go:284-302` | 建议 | 同一文件内两套固定窗口实现，token 版缺 TTL 自愈、缺 `Retry-After`、用 `context.Background()` |
| S2 | `service/token_routing.go:478` | 建议 | `EstimatedCost` 名不符实，`modelCost` 是跨候选常量，实际只按分组倍率排序 |
| S3 | `service/token_routing.go:34` | 建议 | `RouteAttempt.MaxRatio` 是计划级常量，逐条重复写入审计 |

## 1. 需要修正我此前的几处判断

先纠正上一轮口头审查里说错的内容，避免误导后续施工：

**固定窗口不是本次引入的回归。** `redisFixedWindowScript` 及"禁止改回滑动窗口"的注释来自上游 `31d70fca3`（`refactor(auth): replace dashboard sessions with stateless tokens`），早于本次改动。token 限流器改成固定窗口是在**对齐**仓库既有约定，不是破坏它。真正的问题是键名迁移（见 P1）。

**`ContextKeyAutoGroup` 无条件写入是修复，不是隐患。** 旧代码 `if info.TokenGroup == "auto"` 才写，意味着固定分组的 Key 跨组容灾后，`service/quota.go:111` 读不到实际分组，会按原 token 分组倍率结算。改成无条件写入后 `auto_group` 恒等于真实选中分组，结算才正确。`quota.go` 的 `exists` 分支现在恒为真，但取到的值是对的，无副作用。

**预扣费循环的上下文恢复是正确的。** `maxTokenRoutePreConsumePriceData` 进入前 `relayInfo.UsingGroup` 已被 `relay.go:155` 那次 `ModelPriceHelper` → `HandleGroupRatio` 同步为 distributor 选中的分组，因此 `selectedGroup` 捕获到的是真实原值，循环后对 context 和 `relayInfo.UsingGroup` 的双向恢复都到位。

**`math/rand` 未显式播种不是问题。** `go.mod` 声明 `go 1.25.1`，Go 1.20+ 全局 `rand` 自动播种，且全局源带锁，并发安全。

**权重求和溢出不是问题。** `Weight` 为 `uint`，累加进 `int`，64 位平台下现实取值无溢出空间。

## 2. P1：Redis 限流键类型冲突

`middleware/rate-limit.go:265` 的键名在改动前后都是 `rateLimit:token:%d`，但底层数据类型变了：

- 旧实现（`9ef661b03`）：`LLen` / `LPush` / `LIndex` / `LTrim`，键是 **LIST**
- 新实现（`tokenRateLimitLua`）：`INCR`，键是 **STRING**

升级部署后，Redis 中残留的旧 LIST 键会让 `INCR` 返回 `WRONGTYPE`，`tokenRedisRateLimiter` 返回 `healthy=false`，`TokenRateLimit` 随即回退到 `inMemoryRateLimiter`。

影响面：

- 每次命中都写一条 `token rate limit redis script failed` 日志，多实例部署下日志量可观；
- 回退期间限流退化为**单机**计数，N 个实例的实际放行量约为配置值的 N 倍，这正是 Key 级限流要防的场景；
- 旧键 TTL 为 `RateLimitKeyExpirationDuration`（`common/constants.go:226`，20 分钟），且新代码不再刷新它，所以窗口是收敛的 —— 最后一次旧代码请求后 20 分钟内自愈。

同文件的 IP／用户限流器在换数据类型时已经用 `redisRateLimitNamespace = "rateLimit:v2"`（`rate-limit.go:16`）规避了同一问题，token 限流器沿用了旧键名，属遗漏。

建议：把键名改为带版本前缀，例如 `fmt.Sprintf("%s:token:%d", redisRateLimitNamespace, tokenID)`，与既有 `redisIPRateLimitKey` / `redisUserRateLimitKey` 保持一致。

## 3. P2：容灾顺序被字典序抹平

`service/token_routing.go:414-440` 的 `routeGroups` 负责决定固定分组 Key 启用容灾后的回退顺序。它先放 token 自身分组，再依次并入 `GetUserAutoGroup(userGroup)` 和 `GetUserUsableGroups(userGroup)`，最后：

```go
sort.Strings(groups[1:])
```

`GetUserAutoGroup`（`service/group.go:46-55`）是**按管理员配置的 `setting.GetAutoGroups()` 顺序**返回的，这个顺序表达的是容灾优先级。`sort.Strings` 会把它连同后续可用分组一起打平成字典序。

后果：`scopePosition` 直接取自 `groups` 切片下标，而 `nextRouteCandidate`（`token_routing.go:266-271`）优先挑最小 `scopePosition`，所以字典序就是实际的容灾切换顺序。管理员把 `vip` 配在 `default` 之前，实际仍会先切 `default`。

需要说明两点：

- 排序本身有其必要性 —— `GetUserUsableGroups` 返回 map，不排序则迭代顺序随机，容灾顺序不可复现。作者是在用字典序换确定性。
- 该问题只影响 `Strategy == "priority"`。`price` 策略下 `sortRouteCandidatesByPrice` 会重排全部候选，`scopePosition` 不参与决策。

建议：保留确定性但换掉排序键。按 `GetAutoGroups()` 配置序放入 auto 分组、其余可用分组再按分组倍率升序（倍率相同用名称兜底），既可复现又符合"容灾应优先切到更便宜／更高优先级分组"的设计意图。

## 4. P3：预扣费按最贵候选分组放大

`controller/relay.go:264-288` 的 `maxTokenRoutePreConsumePriceData` 遍历 `GetTokenRouteCandidateGroups(c)` 返回的**全部**候选分组，取其中 `QuotaToPreConsume` 最大者一次性预扣。

这个设计的出发点是对的：预扣一次、覆盖所有可能落地的分组，避免容灾切组后预扣额度不足或多轮重复扣费。设计文档第 5 节也明确写了这一点。

但有个未被记录的副作用：`routeGroupAllowed`（`token_routing.go:442-448`）在 `maxRatio <= 0`（未配置上限）时放行所有分组。于是只要 Key 开了容灾且没设 `max_ratio`，候选里混入一个高倍率分组，**每个请求都会按那个最贵分组的费率锁定额度**。

对余额接近预扣额的用户，`PreConsumeBilling` → `NewBillingSession` 会直接判额度不足并拒绝请求 —— 即使这个请求实际会命中最便宜的分组、真实扣费远低于预扣值。表现为"明明还有余额却报额度不足"。

建议二选一：

- 在设计文档第 5 节补记这一放大效应，并在前端 `max_ratio` 输入处提示"开启容灾建议同时设置最大倍率"；
- 或收窄取最大值的范围：只对本次 `RoutePlan` 里实际存在候选渠道的分组求最大值（当前 `GetTokenRouteCandidateGroups` 已经是从 `plan.Candidates` 去重得到的，但它包含了全部容灾层级的分组，可考虑只取 `scopePosition` 最小的若干层）。

## 5. 建议项

### S1：两套固定窗口实现应合并

`rate-limit.go` 里 `redisFixedWindowScript`（第 23 行）与 `tokenRateLimitLua`（第 284 行）做同一件事，但 token 版缺了三样东西：

- **TTL 自愈**：前者有 `if ttl < 0 then EXPIRE` 分支，后者只在 `count == 1` 时设置 TTL。若键因异常丢失 TTL，计数将永久停留在超限状态，该 Key 被永久限流。
- **`Retry-After`**：前者返回 `{allowed, count, ttl}`，调用方 `writeRateLimited` 据此下发 `Retry-After` 头；`TokenRateLimit`（第 270 行）只 `c.Status(429)` + `Abort()`，客户端拿不到退避提示。
- **请求上下文**：`redisFixedWindowTake` 用 `c.Request.Context()`，`tokenRedisRateLimiter` 用 `context.Background()`，客户端断连后 Redis 调用不会随请求取消。

建议让 `TokenRateLimit` 直接复用 `redisFixedWindowTake` + `writeRateLimited`，删掉 `tokenRateLimitLua`。注意 `TokenRateLimit` 需要保留"Redis 异常时回退内存限流"这一语义（来自 `9ef661b03`），而 `redisRateLimiter` 现在的行为是报 500，合并时需要把回退分支保留在 token 侧。

### S2：`EstimatedCost` 命名与实现不符

`token_routing.go:465-478`：

```go
modelCost := 1.0
if modelPrice, usePrice := ratio_setting.GetModelPrice(modelName, false); usePrice {
    modelCost = modelPrice
} else if modelRatio, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
    modelCost = modelRatio
}
// ...
EstimatedCost: modelCost * ratio,
```

同一次请求里 `modelName` 固定，所以 `modelCost` 对所有候选是同一个常量，乘上去不改变任何排序结果 —— 排序实际只由 `ratio`（`GetUserGroupRatio`）决定。

而且 `modelCost` 混用了两种量纲：`GetModelPrice` 是按次价格，`GetModelRatio` 是按 token 倍率，两者不可比。当前因为是常量所以无害，但一旦将来引入渠道级价格覆盖，这个字段会给出错误的成本序。

建议要么把字段改名为 `GroupRatio` 语义的排序键（`GroupRatio` 字段已存在，可直接用它排序并删掉 `EstimatedCost`），要么在注释里写明"仅用于同模型跨分组比较，不是绝对成本"。

### S3：审计条目冗余字段

`RouteAttempt.MaxRatio`（`token_routing.go:34`）在 `consumeRouteCandidate` 里逐条写入 `plan.MaxRatio`，而它是计划级常量。日志 `other.admin_info` 里已经通过 `AppendTokenRouteAdminInfo` 单独输出了 `max_ratio`（第 394-396 行），逐条重复会放大日志体积。

建议从 `RouteAttempt` 移除该字段，只保留计划级的那一份。

## 6. 已验证正确的部分

- **候选去重**：`consumedChannelIDs` 保证同一渠道不会被重试命中两次；`preparedCandidateID` 的交接逻辑正确 —— distributor 侧 `selectRouteCandidate(plan, false)` 不消费，`getChannel(retry=0)` 时再校验渠道状态并消费，中途渠道被禁用会记 `channel_unavailable` 并顺延到下一候选。
- **加权选择无空切片风险**：`selectWeightedRouteCandidate` 的入参 `weighted` 恒非空 —— `available` 非空，`scope` 取自其中的最小值，`priority` 取自该 scope 内的最大值，至少有一个候选同时满足，故 `rand.Intn(len(...))` 不会 panic。
- **零权重处理**：全零权重时退化为均匀随机，非全零时零权重候选永不被选中（`randomWeight -= 0` 不会转负），符合渠道权重语义。
- **审计落位符合项目约定**：`route_attempts` 挂在 `admin_info` 下，非管理员日志视图会自动剥离，与 `AGENTS.md` 中 `quota_saturation` 的处理方式一致。
- **计费安全**：预扣费路径未引入裸类型转换，`PreConsumeBilling` 的 `QuotaClamp` 与负额度校验（`service/billing.go:21-36`）仍在链路上，`maxQuota` 初值 `-1` 仅用于选最大值、不会作为预扣额传出（候选非空时必被覆盖，候选为空时走 `ModelPriceHelper` 分支提前返回）。
- **放大的预扣额不会污染结算**：`maxPriceData` 只作为预扣金额使用。真实选路后 `refreshTokenRouteBilling`（`controller/relay.go:293-310`）会按实际分组重新计算 `priceData` 并覆盖 `relayInfo.PriceData`，已有预扣会走 `Billing.Reserve` 补差而非重复扣费，最终结算用的是实际命中分组的倍率。P3 是额度被临时锁定过多的问题，不是多收费的问题。

## 7. 验证记录

```
go build ./...                                              # 通过
go test ./service/ -run 'TokenRoute|RouteCandidate' -v      # 4 passed
```

测试用例本身是确定性的 —— `TestPriorityRouteCandidateUsesWeightWithinPriority` 里权重 0/100 的组合下加权选择结果唯一，不依赖随机数取值。

未覆盖（设计文档第 8 节亦标注为待补）：SQLite／MySQL／PostgreSQL 三库迁移验证、Redis 限流集成测试、前端路由编辑器交互测试。P1 的键类型冲突正属于需要 Redis 集成测试才能提前发现的一类问题。
