# ZeroClawDash

## 项目概述

- **目标**：为运行在玩客云（Armbian/Linux ARMv7）上的 ZeroClaw 提供一个极简、高效的 Web 管理面板。
- **核心痛点**：解决 Headless 环境下修改 `~/.zeroclaw/config.toml` 繁琐、易错且无法实时查看日志的问题。
- **交付物**：单个静态编译的 Go 二进制文件（内嵌 Web 静态资源）。

***

## 参考文档

- **ZeroClaw CLI 命令参考**：<https://github.com/zeroclaw-labs/zeroclaw/blob/master/docs/i18n/zh-CN/reference/cli/commands-reference.zh-CN.md>
## 功能特性

- 系统监控：实时 CPU、内存、服务运行时间
- 服务控制：启动、停止、重启 ZeroClaw 服务
- 配置管理：可视化配置修改
- 实时日志：SSE 日志流
- 版本更新：一键升级

## 部署步骤

### 1. 下载安装包

从 GitHub Releases 下载：

地址：<https://github.com/guikeyy/ZeroClawDash/releases/>

可以从上面地址拿到版本号，直接在玩客云中进行下载，命令（注意替换版本号）：

```bash
wget https://github.com/guikeyy/zeroclawdash/releases/latest/download/zeroclawdash-{version}-linux-armv7.tar.gz
```

或手动下载后通过 SCP 上传到玩客云：

```bash
scp zeroclawdash-{version}-linux-armv7.tar.gz root@<玩客云IP>:/root/
```

### 2. 解压安装

```bash
tar -xzf zeroclawdash-{version}-linux-armv7.tar.gz
chmod +x zeroclawdash manage.sh
mv zeroclawdash /跟zeroclaw # 移动到同一个目录下
```

### 3. 管理服务

```bash
./manage.sh start    # 启动服务
./manage.sh stop     # 停止服务
./manage.sh status  # 查看状态
./manage.sh restart # 重启服务
```

### 4. 访问 Web 界面

```
http://<玩客云IP>:42611
```

## API 接口

| 接口                     | 方法       | 说明      |
| ---------------------- | -------- | ------- |
| `/`                    | GET      | Web 界面  |
| `/api/system/status`   | GET      | 系统状态    |
| `/api/config`          | GET/POST | 配置读取/保存 |
| `/api/service/control` | POST     | 服务控制    |
| `/api/logs`            | GET      | SSE 日志流 |
| `/api/update`          | POST     | 版本更新    |

## 配置文件

ZeroClaw：`~/.zeroclaw/config.toml`

## 注意事项

1. 确保 ZeroClaw 已正确安装
2. 确保 journalctl 服务可用
3. 更新功能需要网络访问 GitHub API

