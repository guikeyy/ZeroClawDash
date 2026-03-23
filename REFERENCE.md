# ZeroClawDash 参考文档

## 1. 项目概述

- **目标**：为运行在玩客云（Armbian/Linux ARMv7）上的 ZeroClaw 提供一个极简、高效的 Web 管理面板。
- **核心痛点**：解决 Headless 环境下修改 `~/.zeroclaw/config.toml` 繁琐、易错且无法实时查看日志的问题。
- **交付物**：单个静态编译的 Go 二进制文件（内嵌 Web 静态资源）。

***

## 2. 参考文档

- **ZeroClaw CLI 命令参考**：<https://github.com/zeroclaw-labs/zeroclaw/blob/master/docs/i18n/zh-CN/reference/cli/commands-reference.zh-CN.md>

***

## 3. 核心功能模块

### 3.1 系统监控与服务控制

- **监控指标**：实时采集并展示 CPU 使用率、内存占用、服务运行时间（Uptime）。
- **生命周期管理**：
  - **Start/Stop/Restart**：通过 Go 调用 `systemctl` 或直接操作进程。
  - **状态反馈**：前端需通过长轮询或 SSE 实时更新服务状态标签（Running/Stopped）。

### 3.2 自动化版本管理

- **版本检测**：后端请求 GitHub API，对比本地二进制版本与云端最新 Release。
- **云端地址**：`https://github.com/zeroclaw-labs/zeroclaw/releases`
- **增量更新逻辑**：
  1. 用户触发更新。
  2. 后端通过正则匹配下载玩客云专用包：`zeroclaw-{version}-armv7-unknown-linux-gnueabihf.tar.gz`（版本号动态获取）。
  3. **安全替换**：
     - 备份当前二进制文件：`cp ./zeroclaw ./zeroclaw.bak`
     - 停止当前服务：`./zeroclaw service stop`
     - 解压新版包：`tar -xzf zeroclaw-armv7-unknown-linux-gnueabihf.tar.gz`
     - 覆盖旧二进制文件：`cp zeroclaw ./zeroclaw`
     - 赋予执行权限：`chmod +x ./zeroclaw`
     - 启动服务：调用后端 `/api/service/control` 接口，参数 `action: start`
     - 验证服务状态：调用后端 `/api/system/status` 接口确认服务状态

### 3.3 参数化配置中心 (核心改动)

取消繁琐的全文本编辑，改为表单驱动模式，确保配置文件符合 ZeroClaw 规范。

- **配置文件路径**：`~/.zeroclaw/config.toml`
- **表单字段映射**：
  - **Protocol Type**: 下拉菜单选择（OpenAI 兼容 / Anthropic 兼容）。
  - **API URL**: 文本框输入 Endpoint。
  - **API Key**: 密码框输入（可选）。
  - **Default Model**: 文本框输入模型 ID（可选）。
- **配置转换规则**：
  - 后端读取现有 `config.toml` 文件内容
  - 只修改以下 3 个 key，不增加任何新内容：
    - `default_provider`: 根据协议类型生成，格式为 `"custom:https://xxx"` 或 `"anthropic-custom:https://xxx"`
    - `api_key`: 如果用户填写了 API Key，后端会检查 config.toml 中是否存在该 key，如果不存在则生成，如果存在则更新
    - `default_model`: 如果用户填写了模型名称，后端会检查 config.toml 中是否存在该 key，如果不存在则生成，如果存在则更新
- **测试与保存逻辑 (Atomic Update)**：
  1. **备份**：将 `config.toml` 复制为 `config.toml.bak`。
  2. **读取与修改**：读取现有配置文件，仅更新上述 3 个 key 的值，保留文件原有结构和其他配置项。
  3. **校验**：执行 `zeroclaw agent -m "Hello, ZeroClaw!"`。
  4. **回滚**：若校验失败，隔3s进行重试，如果重试失败，则立即还原备份文件并向前端返回报错信息。

### 3.4 实时终端日志

- **技术实现**：基于 **SSE (Server-Sent Events)**。
- **数据源**：后端读取 `journalctl -u zeroclaw -f -n 100` 或直接 tail 日志文件。
- **交互**：支持日志滚动锁定、关键词过滤及清空显示。

***

## 4. 技术规范

### 4.1 架构设计

- **后端 (Go)**：使用 `net/http` 标准库，减少依赖。利用 `//go:embed` 打包前端。
- **前端 (Vanilla)**：单 HTML 文件。样式使用 Tailwind CSS (CDN)，图标使用 Lucide，交互使用原生 JS。
- **通信协议**：RESTful API 用于控制与配置，SSE 用于日志。

### 4.2 接口协议定义 (API)

| 动作       | 路径                     | 方法     | 说明                                                                                                                                                                        |
| :------- | :--------------------- | :----- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **状态获取** | `/api/system/status`   | GET    | 返回 CPU, MEM, Service Status                                                                                                                                               |
| **配置保存** | `/api/config`          | POST   | 接收表单 JSON，执行备份、转换、校验、写入                                                                                                                                                   |
| **服务控制** | `/api/service/control` | POST   | 参数：`action: start/stop/restart` - **ZeroClaw CLI 命令参考**：<https://github.com/zeroclaw-labs/zeroclaw/blob/master/docs/i18n/zh-CN/reference/cli/commands-reference.zh-CN.md> |
| <br />   | <br />                 | <br /> | <br />                                                                                                                                                                    |
| **日志流**  | `/api/logs`            | GET    | 返回 `text/event-stream` 日志流                                                                                                                                                |

***

## 5. UI/UX 设计要求

1. **状态可视化**：
   - 服务运行时，状态灯显示绿色脉冲动画。
   - 保存配置时，按钮显示 Loading 状态，避免重复提交。
2. **容错提示**：`zeroclaw --status` 返回的错误必须在前端以红字完整展示，方便排查 URL 格式或 Key 错误。

***

## 6. 开发路线图 (Roadmap)

- **Phase 1**: 实现 Go 后端的基础 Web Server 及 HTML 页面静态输出。
- **Phase 2**: 实现 `config.toml` 的读写逻辑及 `custom:` 规则转换。
- **Phase 3**: 对接 SSE 日志流与系统资源监控。
- **Phase 4**: 实现一键升级逻辑及 GitHub API 对接。

