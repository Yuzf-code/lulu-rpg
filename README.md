# Lulu RPG 🎲

基于 Web 的**对话式 RPG 引擎**：创建角色卡、设定你自己的档案，选择一位或多位角色开启一局游戏。剧情写手会以「旁白 / 角色台词 / 动作 / 内心」等不同视角流式讲述故事，你可以选择**以角色身份说话**，或以**导演身份**只描述剧情走向。

- **Go 后端 + 无构建步骤的原生前端**（移动端优先，桌面端为「左图右文」布局）
- **SQLite 存储**，数据全部落盘在 `data/` 目录
- **Docker Compose 一键部署**；LLM / 图像服务均为 **OpenAI 兼容接口**，可接 Ollama、vLLM、llama.cpp server、各类聚合网关
- **内置演示模式（Mock）**：不配置任何模型也能完整体验全流程

## 功能总览

| 模块 | 说明 |
| --- | --- |
| 角色卡 | 名称 / 头衔 / 性格 / 背景 / 外貌 / 标签 / 关系 / 开场白 / 对话示例；头像支持**上传**或 **AI 生成**（按名称+外貌） |
| 文本蒸馏 | 粘贴小说/跑团记录等原文，**蒸馏出角色卡草稿**（可指定 1~5 个对象，留空自动识别）；可指定**主体**，蒸馏对象对主体的态度/关系；编辑已有卡时可选择**增强合并** |
| AI 草稿 | 开场白、对话示例可一键生成草稿，导入表单后自由修改 |
| 行动灵感 | 输入框旁「✨ 灵感」按钮：按当前剧情生成 4~6 条建议（台词与导演指令混合），点选即填入输入框 |
| 角色档案 | 玩家在故事中的身份；写手会配合档案演出，绝不代替你发言 |
| 游戏实例 | 一个对话即一局游戏，相互隔离；可携带场景设定 |
| 多角色 | 一局可选多位角色同台；写手按视角归属叙事（与角色无关的场面用「旁白」） |
| 双模输入 | 「台词」= 角色发言；「导演」= 剧情走向指令（不进入剧情正文） |
| 流式响应 | SSE 逐段推送，段落即产即显；可随时点击停止 |
| 派生私聊 | 从主线中选一位/几位角色单独开新对话：私聊**继承主线记忆**，且能动态感知主线后续推进；反过来，私聊中的相处会作为**私下经历回流主线**——只有参与私聊的角色知情，其他角色（写手会严格遵守）不知情 |
| 记忆管理 | 近期消息窗口 + 超出阈值自动**滚动摘要**，适配 27B 级小模型的上下文预算 |
| 自动配图 | 每轮可按剧情自动生成一幅插画；界面左侧展示最新图、下方缩略图历史 |

## 快速开始

### 方式一：Docker Compose（推荐）

```bash
# 1) 演示模式，开箱即玩
docker compose up -d --build
# 打开 http://localhost:8080

# 2) 接入真实模型（示例：内置 Ollama）
cp .env.example .env
# 编辑 .env：
#   LLM_PROVIDER=openai
#   LLM_BASE_URL=http://ollama:11434/v1
#   LLM_MODEL=qwen3:8b          # 27B 级可换 Qwen2.5-32B 等
docker compose --profile ollama up -d
docker compose exec ollama ollama pull qwen3:8b
```

### 方式二：本地开发

```bash
go run ./cmd/server       # 自动加载 .env；无 .env 时为演示模式
go test ./...             # 全量测试
```

### 手机局域网访问 📱

服务默认绑定所有网卡，手机连同一 Wi-Fi 即可游玩：

1. `.env` 中设置 `APP_PORT=8081`
2. 查询电脑局域网 IP：macOS `ipconfig getifaddr en0` / Linux `ip addr`
3. 手机浏览器访问 `http://<电脑IP>:8081`

提示：
- macOS 首次启动如弹出防火墙询问，选择「允许」；
- LLM/图像请求默认**直连**（不读系统的 http_proxy，避免局域网服务被本机代理劫持）；如需经代理访问外部 API，设置 `APP_HTTP_PROXY=http://127.0.0.1:7897`；
- Ollama 默认上下文较小（约 4k），若调大了 `CONTEXT_MAX_TOKENS`，请在 Ollama 侧同步设置 `OLLAMA_CONTEXT_LENGTH`（或建模型时指定 `num_ctx`），否则长提示词会被截头。

## 接入大模型 / 图像服务

后端只要求 **OpenAI 兼容协议**，按环境变量指定：

| 环境变量 | 说明 |
| --- | --- |
| `LLM_PROVIDER` | `mock`（默认演示）/ `openai` |
| `LLM_BASE_URL` | 如 `http://ollama:11434/v1`、`http://host.docker.internal:8000/v1` |
| `LLM_API_KEY` | 无鉴权可留空 |
| `LLM_MODEL` | 模型名 |
| `IMG_MODEL` | 留空=关闭配图；`mock`=占位画师；否则走 `IMG_BASE_URL` 的 `/images/generations` |
| `IMG_BASE_URL` / `IMG_API_KEY` / `IMG_SIZE` | 图像服务接入点 |

其他可调项（上下文长度、摘要阈值、温度等）见 `.env.example`。

**剧情写手输出协议**：模型被约束逐行输出 `[旁白]…`、`[角色名·动作]…`、`[角色名·内心]…`、`[角色名]台词` 格式，服务端流式解析为「带归属的分段」；未打标签但以已知角色名开头的行会被自动纠正。该协议对 JSON 能力较弱的中小模型非常稳健。

## 目录结构

```
cmd/server/          入口
internal/config/     环境变量配置
internal/store/      SQLite：角色卡/档案/会话/消息/图片 + 演示数据播种
internal/llm/        LLM Provider（OpenAI 兼容流式 + Mock 写手）
internal/imggen/     图像 Provider（OpenAI 兼容 + 占位画师）
internal/game/       引擎：分段流式解析、提示词/上下文组装、滚动摘要记忆、回合编排
internal/api/        HTTP API + SSE + 静态资源
web/                 前端 SPA（原生 ES Module，无构建步骤）
```

## API 概览

```
GET    /api/health                       GET   /api/config
GET|POST /api/characters                 GET|PUT|DELETE /api/characters/{id}
POST   /api/characters/{id}/avatar       POST  /api/avatars/generate
POST   /api/characters/distill           POST  /api/characters/{id}/distill
POST   /api/characters/generate-draft
GET|POST /api/personas                   GET|PUT|DELETE /api/personas/{id}
POST   /api/personas/{id}/default
GET|POST /api/sessions                   GET|PATCH|DELETE /api/sessions/{id}
GET    /api/sessions/{id}/messages       POST  /api/sessions/{id}/opening   (SSE)
POST   /api/sessions/{id}/turn   (SSE)  POST  /api/sessions/{id}/inspiration
POST   /api/uploads                      POST  /api/images/generate
```

## 说明

- 未做登录/注册/审核等平台功能（按需求从简）；单实例部署，数据即 `data/` 目录，备份拷贝即可。
- 同一会话同时只允许一轮生成（并发请求返回 409），点击「停止」会保留已完成分段。
- 私聊与主线的记忆是**双向但隔离**的：私聊上下文动态取主线的最新摘要；主线中每个角色只携带自己参与过的私聊摘要（存于该私聊会话的 `summary`，由滚动摘要机制维护），写手被明确约束「其他角色不应表现出知情」。
