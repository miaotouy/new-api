# 渠道测试请求内容配置设计

## 1. 背景

渠道连接测试目前由后端按端点构造固定请求，测试页只能选择模型、端点类型和流式模式。当前内置内容包括：

- Chat、Anthropic、Gemini：`hi`；
- OpenAI Responses、Responses Compact：`hi`；
- Embeddings：`hello world`；
- Image Generation：`a cute cat`；
- Jina Rerank：固定的查询和文档列表。

不同端点的测试内容语义不同，不能使用同一个全局字符串同时表示消息、向量输入、图片提示词和重排数据。因此，本功能按端点隔离配置，只开放测试内容字段，不开放完整原始 JSON 请求体。

## 2. 目标与非目标

### 2.1 目标

- 在渠道测试页按需打开“测试内容”设置，不常驻占用测试页空间；
- 每种端点保留独立的系统内置预设和自定义内容；
- 支持只应用于当前测试弹窗生命周期的临时覆盖；
- 支持将自定义内容保存为渠道默认配置；
- 快捷测试、单模型测试、批量模型测试和定时渠道测试使用一致的配置解析规则；
- 保持现有 GET 测试接口和未配置渠道的测试行为兼容；
- 继续复用现有模型映射、协议转换、参数覆盖和响应校验流程。

### 2.2 非目标

第一版不提供以下能力：

- 编辑完整原始 JSON 请求体；
- 自定义 `model`、`stream`、`max_tokens`、`temperature` 等请求参数；
- 自定义图片数量、尺寸和质量；
- 自定义 Rerank 的 `top_n`；
- 多轮聊天、任意角色消息、工具调用、文件或多模态内容；
- 将渠道连接测试扩展为完整 Playground。

这些字段继续由系统控制，避免绕过既有协议校验和计费安全边界。需要调整其他请求参数时，仍使用渠道“参数覆盖”能力。

## 3. 核心设计

### 3.1 按端点隔离

配置键使用最终解析出的 `constant.EndpointType`，而不是渠道供应商类型。支持的端点和可编辑字段如下：

| 端点 | 配置键 | 可编辑字段 | 系统内置预设 |
| --- | --- | --- | --- |
| OpenAI Chat | `openai` | `content` | `hi` |
| Anthropic Messages | `anthropic` | `content` | `hi` |
| Gemini Generate Content | `gemini` | `content` | `hi` |
| OpenAI Responses | `openai-response` | `content` | `hi` |
| Responses Compact | `openai-response-compact` | `content` | `hi` |
| Embeddings | `embeddings` | `input` | `hello world` |
| Image Generation | `image-generation` | `prompt` | `a cute cat` |
| Jina Rerank | `jina-rerank` | `query`、`documents` | 当前固定查询和两条文档 |

即使多个端点当前使用相同的 `hi`，也必须分别保存覆盖。以后修改某一个端点的预设或输入结构时，不应影响其他端点。

### 3.2 三层配置与优先级

系统维护三层配置：

1. 本次测试弹窗的端点覆盖；
2. 渠道保存的端点默认配置；
3. 后端定义的端点内置预设。

解析优先级为：

```text
本次测试端点覆盖 > 渠道端点默认配置 > 系统内置预设
```

数据库只保存自定义端点，不复制系统内置预设。这样系统以后调整内置预设时，未自定义的渠道会自动使用新值。

临时覆盖需要区分三种状态：

- `inherit`：不发送该端点覆盖，沿用渠道配置；
- `builtin`：显式忽略渠道配置，本次测试使用系统内置预设；
- `custom`：本次测试使用请求携带的自定义内容。

### 3.3 自动检测和批量测试

端点解析必须由后端负责，前端不能复制模型名称和渠道类型的判断逻辑。

当 Endpoint Type 为“自动检测”时，同一批模型可能解析到不同端点。例如 Embedding 模型使用 `embeddings` 配置，Codex 模型使用 `openai-response` 配置，普通模型使用 `openai` 配置。因此临时测试请求携带的是按端点组织的覆盖映射，而不是单个内容对象。

每个模型的处理顺序为：

```text
模型和渠道信息
  -> 解析最终测试端点
  -> 按最终端点选择临时覆盖、渠道配置或内置预设
  -> 构造标准测试 DTO
  -> 模型映射
  -> 转换为上游协议
  -> 应用渠道参数覆盖
  -> 发送请求
```

渠道参数覆盖仍在协议转换后执行，因此它可以覆盖本功能生成的测试内容。UI 需要提示这一优先级。

## 4. 数据结构

### 4.1 渠道持久配置

在 `model.Channel` 增加独立的 `TEXT` 字段：

```go
TestRequestConfig string `json:"test_request_config" gorm:"type:text"`
```

选择独立字段而不是写入 `Channel.OtherSettings` 的原因：

- 避免更新嵌套 JSON 时覆盖并发修改的其他渠道设置；
- 定时测试读取直接，不需要读改写整个 `settings`；
- 渠道复制、导入导出和权限字段审计更明确；
- `TEXT` 可同时用于 SQLite、MySQL 和 PostgreSQL，且不需要数据库默认值。

字段内容带版本号，只保存自定义项：

```json
{
  "version": 1,
  "overrides": {
    "openai": {
      "content": "Please reply with OK only."
    },
    "image-generation": {
      "prompt": "A red cube on a white background."
    },
    "jina-rerank": {
      "query": "What is deep learning?",
      "documents": [
        "Deep learning uses multi-layer neural networks.",
        "A database stores and retrieves structured data."
      ]
    }
  }
}
```

`version` 用于未来扩展结构；未知版本必须返回明确配置错误，不能静默按 v1 解析。

### 4.2 后端 DTO

后端使用显式结构并按端点校验，不把配置直接透传给上游：

```go
type ChannelTestRequestConfig struct {
    Version   int                                   `json:"version"`
    Overrides map[string]ChannelTestContentOverride `json:"overrides"`
}

type ChannelTestContentOverride struct {
    Mode      string   `json:"mode,omitempty"`
    Content   *string  `json:"content,omitempty"`
    Input     *string  `json:"input,omitempty"`
    Prompt    *string  `json:"prompt,omitempty"`
    Query     *string  `json:"query,omitempty"`
    Documents []string `json:"documents,omitempty"`
}
```

持久配置不写 `mode`，存在的端点均视为 `custom`。测试接口中的临时配置使用 `mode` 表达 `builtin` 或 `custom`；`inherit` 通过不传该端点实现。

## 5. 后端接口

### 5.1 保留兼容接口

继续支持：

```http
GET /api/channel/test/:id?model=...&endpoint_type=...&stream=true
```

GET 接口不接受临时内容，使用“渠道默认配置 > 系统内置预设”。现有 Classic 页面和外部调用无需立即迁移。

### 5.2 执行带临时配置的测试

新增同路径 POST：

```http
POST /api/channel/test/:id
Content-Type: application/json
```

请求示例：

```json
{
  "model": "gpt-4o-mini",
  "endpoint_type": "auto",
  "stream": false,
  "test_request_overrides": {
    "openai": {
      "mode": "custom",
      "content": "Please reply with OK only."
    },
    "embeddings": {
      "mode": "builtin"
    }
  }
}
```

响应在保持现有字段兼容的基础上增加实际解析端点：

```json
{
  "success": true,
  "message": "",
  "time": 0.42,
  "endpoint_type": "openai"
}
```

### 5.3 获取设置面板数据

打开“测试内容”面板时按需请求：

```http
GET /api/channel/test/:id/config
```

返回系统内置预设、渠道已保存覆盖和配置版本。系统内置预设以后端为唯一数据源，前端不得维护另一份内容常量。

### 5.4 保存渠道默认配置

```http
PUT /api/channel/test/:id/config
Content-Type: application/json
```

请求体为完整的 v1 持久配置。保存内置预设等价于从 `overrides` 中删除对应端点。接口只更新 `test_request_config`，不能用测试页中可能过期的渠道对象覆盖其他字段。

权限建议：

- GET、POST 测试：`authz.ChannelOperate`；
- PUT 渠道默认配置：`authz.ChannelWrite`。

路由注册时，静态和更具体的 `/test/:id/config` 路由应避免被现有 `/test/:id` 参数路由错误匹配，并补充路由冲突测试。

## 6. 后端实现

### 6.1 统一端点解析

当前请求路径选择和 `buildTestRequest` 各自包含一部分自动检测判断。实现前先提取一个权威解析函数：

```go
func resolveChannelTestEndpoint(
    channel *model.Channel,
    modelName string,
    requestedEndpoint string,
) (constant.EndpointType, error)
```

该函数覆盖现有 Rerank、Embedding、VolcEngine Seedream、Codex、Responses Compact 和默认 OpenAI Chat 行为。请求路径、配置查找和测试 DTO 构造都使用它的返回值，避免多处判断漂移。

### 6.2 内置预设与请求构造

内置预设集中定义在后端，并返回全新的值或深拷贝，调用方不得修改共享对象。构造请求时使用结构化 DTO；Responses 的 `json.RawMessage` 必须通过 `common.Marshal` 生成，不能拼接用户字符串。

内容映射如下：

- Chat、Anthropic、Gemini：`GeneralOpenAIRequest.Messages[0].Content`；
- Responses、Responses Compact：输入项的 `content`；
- Embeddings：`EmbeddingRequest.Input[0]`；
- Image Generation：`ImageRequest.Prompt`；
- Rerank：`RerankRequest.Query` 和 `Documents`。

模型、流式开关、输出 token 限制、图片数量和尺寸、Rerank `top_n` 继续使用现有系统值。

### 6.3 配置解析失败

保存配置时完成完整校验。运行测试时如果数据库中已有配置无法解析：

- 手动测试返回明确的渠道测试配置错误；
- 定时测试记录错误，并按现有测试失败策略处理；
- 不静默回退到内置预设，避免管理员误以为自定义内容已生效。

### 6.4 缓存、复制与权限字段

- 更新配置后使该渠道缓存失效或刷新缓存；
- 复制渠道时复制 `test_request_config`；
- 批量导入导出若包含渠道完整配置，应同步支持该字段；
- 新字段加入渠道字段权限分类测试；
- 不添加 GORM 布尔默认标签或数据库专用 JSON 类型。

## 7. 校验与安全边界

后端是最终校验边界，前端校验只用于即时反馈。

建议限制：

- POST 请求体最大 64 KiB；
- 最多包含当前支持的 8 个端点键；
- `content`、`input`、`prompt`、`query`：最多 4096 个 Unicode 字符；
- Rerank `documents`：1 到 8 条；
- 每条文档最多 4096 个 Unicode 字符；
- 文档总长度最多 16384 个 Unicode 字符；
- 自定义字符串去除首尾空白后不得为空，但发送时保留用户原始内容；
- 拒绝未知端点、未知 mode、未知字段和不属于该端点的字段；
- `builtin` 模式不能同时携带内容字段；
- 测试内容不写入普通系统日志、错误消息或响应正文。

测试内容可能包含内部提示词，日志只能记录配置来源、端点和长度，例如：

```text
endpoint=openai source=session_custom content_length=28
```

不能记录实际文本。

## 8. 前端交互

### 8.1 测试页入口

测试页不常驻内容输入框。在 Endpoint Type 和 Stream Mode 控件附近增加带设置图标的“测试内容”按钮：

```text
[Endpoint Type] [Stream Mode] [设置图标 测试内容]
```

按钮状态：

- 没有临时覆盖时显示普通状态；
- 存在临时覆盖时显示“已调整”状态或数量徽标；
- 渠道仅有持久配置但本次未调整时，可显示较弱的“渠道配置”提示。

点击后打开右侧 Sheet；移动端使用全宽 Sheet。沿用当前渠道测试弹窗内已有的 Sheet 组合方式，不新增 UI 依赖。

### 8.2 设置面板

显式选择端点时，面板默认只展示该端点。选择“自动检测”时，面板提供端点下拉选择，并提示每个模型会按后端解析出的实际端点选取配置。

每个端点维护以下会话来源状态：

- 沿用渠道配置；
- 本次使用内置预设；
- 本次自定义。

面板展示当前有效预设。选择“本次自定义”后才显示输入控件：

- 文本类、Embedding、图片端点使用多行文本框；
- Rerank 使用 Query 文本框和可增删、可排序的 Documents 列表；
- 显示字符数和校验错误；
- Endpoint 切换不清空其他端点尚未应用的草稿。

底部操作：

- “应用到本次测试”：更新测试弹窗内存状态，不写数据库；
- “保存为渠道默认”：使用 PUT 接口保存当前端点，需要渠道写权限；
- “取消”：丢弃本次打开面板后的草稿。

“应用到本次测试”为主操作。“保存为渠道默认”为次要操作，并明确提示会修改渠道配置。保存成功后，该端点的会话来源恢复为“沿用渠道配置”。

### 8.3 生命周期

- 打开渠道测试弹窗时，会话覆盖为空；
- 关闭测试内容 Sheet 不等于关闭渠道测试弹窗；
- 关闭渠道测试弹窗后丢弃全部会话覆盖；
- 单模型测试和批量测试共用当前会话覆盖映射；
- 切换 Endpoint Type 不删除已经编辑的其他端点覆盖；
- 切换渠道时重新加载对应渠道配置，不能复用上一个渠道的状态。

### 8.4 国际化和可访问性

- 所有新增文案使用 `useTranslation()`；
- 按项目 i18n 流程补齐 `en`、`zh`、`fr`、`ja`、`ru`、`vi`，并遵循仓库现有 `zh-TW` 同步策略；
- 图标按钮提供可访问名称和 Tooltip；
- Sheet 打开后焦点进入标题或首个表单控件，关闭后返回“测试内容”按钮；
- Documents 的新增、删除和排序操作均可通过键盘完成。

## 9. 前端状态与 API 类型

前端使用按端点索引的判别联合，避免对错误端点提交错误字段：

```ts
type TestContentSource = 'inherit' | 'builtin' | 'custom'

type SessionTestContentOverride =
  | { endpointType: 'openai'; source: TestContentSource; content?: string }
  | { endpointType: 'anthropic'; source: TestContentSource; content?: string }
  | { endpointType: 'gemini'; source: TestContentSource; content?: string }
  | {
      endpointType: 'openai-response' | 'openai-response-compact'
      source: TestContentSource
      content?: string
    }
  | { endpointType: 'embeddings'; source: TestContentSource; input?: string }
  | {
      endpointType: 'image-generation'
      source: TestContentSource
      prompt?: string
    }
  | {
      endpointType: 'jina-rerank'
      source: TestContentSource
      query?: string
      documents?: string[]
    }
```

序列化请求时：

- `inherit` 不写入 `test_request_overrides`；
- `builtin` 写入 `{ "mode": "builtin" }`；
- `custom` 写入 `{ "mode": "custom", ...内容字段 }`。

配置拉取使用 TanStack Query，并按渠道 ID 缓存。保存成功后只更新或失效对应配置查询，不刷新无关列表。

## 10. 兼容性

- 未配置渠道产生的上游测试请求必须与改造前一致；
- 现有 GET `/api/channel/test/:id` 保留；
- 定时测试不携带临时覆盖，自动读取渠道配置；
- Classic 前端在迁移前继续通过 GET 工作，并能使用已保存的渠道配置；
- Default 前端使用 POST 支持会话覆盖；
- Classic 仍作为受支持主题时，应在发布前补齐相同的“测试内容”入口，或至少提供渠道默认配置的查看和编辑能力；
- 参数覆盖、请求头覆盖和模型映射顺序保持不变。

## 11. 测试计划

### 11.1 后端

1. 权威端点解析覆盖所有现有自动检测分支；
2. 未配置时八种端点生成的请求与当前行为一致；
3. 每种端点能读取自己的渠道配置，不会串用其他端点内容；
4. 会话 `custom` 覆盖渠道配置；
5. 会话 `builtin` 绕过渠道配置；
6. 未传端点覆盖时继承渠道配置；
7. 自动检测批量模型分别选取最终端点配置；
8. Responses 内容中的引号、换行和 Unicode 正确编码；
9. Rerank Query 和 Documents 正确构造；
10. 未知字段、错误端点字段、空白内容和长度超限返回 400；
11. 无权限用户不能保存渠道默认配置；
12. GET 兼容接口、POST 测试接口和配置 GET/PUT 路由无冲突；
13. 配置更新后渠道缓存立即生效；
14. 定时测试使用渠道配置；
15. 参数覆盖在自定义测试内容之后生效；
16. 测试日志不包含实际测试文本；
17. SQLite、MySQL 和 PostgreSQL 均可迁移和保存 `TEXT` 配置。

新增或重写的 Go 测试使用 `testify/require` 和 `testify/assert`。

### 11.2 前端

- 按钮能打开和关闭设置 Sheet；
- 显式端点和自动检测模式展示正确；
- 不同端点草稿相互隔离；
- `inherit`、`builtin`、`custom` 序列化正确；
- Rerank Documents 的添加、删除、排序和校验正确；
- 应用本次测试不会调用保存接口；
- 保存渠道默认后更新查询缓存和来源状态；
- 关闭渠道测试弹窗会清理会话覆盖；
- 批量测试的每个请求携带同一份端点覆盖映射；
- 无写权限时不显示或禁用保存操作；
- 桌面和移动端不存在内容溢出、控件重叠或焦点丢失。

## 12. 实施顺序

1. 重构后端端点解析并增加行为保持测试；
2. 增加配置 DTO、校验、渠道字段和跨数据库迁移；
3. 增加配置 GET/PUT 和测试 POST 接口；
4. 将内置预设接入统一请求构造流程；
5. 实现 Default 前端设置按钮、Sheet 和会话状态；
6. 补齐 i18n、类型检查、Lint、构建和响应式视觉检查；
7. 补齐 Classic 前端入口并进行回归测试；
8. 验证快捷测试、批量测试、全部渠道测试和定时测试。

建议拆分提交，先完成无行为变化的端点解析重构，再引入数据结构和 UI，便于审查和回滚。

## 13. 主要修改位置

后端：

- `controller/channel-test.go`：端点解析、配置选择、请求构造和 POST 测试接口；
- `router/channel-router.go`：POST 测试和配置读写路由；
- `model/channel.go`：持久配置字段、读取与更新；
- `dto/`：测试配置 DTO 和校验；
- `controller/channel_authz.go`：新渠道字段权限分类；
- 渠道复制、导入导出和迁移相关代码。

Default 前端：

- `web/default/src/features/channels/api.ts`：测试配置和 POST 测试 API；
- `web/default/src/features/channels/types.ts`：端点配置类型；
- `web/default/src/features/channels/lib/channel-actions.ts`：传递临时覆盖；
- `web/default/src/features/channels/components/dialogs/channel-test-dialog.tsx`：设置入口和会话状态；
- 新增独立的测试内容 Sheet 组件，避免继续扩大主弹窗组件；
- `web/default/src/i18n/locales/`：新增文案翻译。

Classic 前端：

- `web/classic/src/hooks/channels/useChannelsData.jsx`；
- `web/classic/src/components/table/channels/modals/ModelTestModal.jsx`；
- Classic 对应的 locale 文件。

## 14. 验证命令

```powershell
go test ./controller ./model ./router

cd E:\git\new-api\web\default
bun run typecheck
bun run lint
bun run i18n:sync
bun run build
```

涉及 Classic 前端后，再执行其现有的类型、Lint 和构建脚本。

## 15. 验收标准

- 测试页没有常驻测试内容输入框；
- 用户可通过“测试内容”按钮按需打开设置面板；
- 八种端点拥有互相隔离的内置预设和自定义配置；
- 自动检测和批量测试按每个模型的最终端点选择内容；
- 临时覆盖不会意外写入渠道配置；
- 保存渠道默认后，快捷测试和定时测试无需额外参数即可使用；
- 未配置渠道和旧 GET 调用的行为不回归；
- 不允许通过该功能修改完整请求或计费相关乘数；
- 测试内容不会出现在普通日志中；
- 后端测试、前端类型检查、Lint、生产构建和 i18n 检查全部通过。
