# ZeroClawDash 部署指南

## 部署步骤

### 1. 下载二进制文件

从 GitHub Releases 下载适用于玩客云 ARMv7 架构的二进制文件：

```bash
wget https://github.com/guikeyy/zeroclaw/releases/latest/download/zeroclawdash-armv7
```

或者手动下载后通过 SCP 上传到玩客云：

```bash
scp zeroclawdash root@<玩客云IP>:/root/
```

### 2. 安装到系统路径

```bash
chmod +x zeroclawdash
mv zeroclawdash /zeroclaw的文件夹下 # 把二进制移动到跟
```

### 3. 使用管理脚本

使用提供的 `manage.sh` 脚本进行进程管理：

```bash
chmod +x manage.sh
```

**启动服务：**
```bash
./manage.sh start
```

**停止服务：**
```bash
./manage.sh stop
```

**查看状态：**
```bash
./manage.sh status
```

**重启服务：**
```bash
./manage.sh restart
```

### 4. 静默运行说明

管理脚本已配置为静默运行模式，使用 `nohup` 方式启动，断开终端后进程会继续运行。

如需手动静默运行：

```bash
nohup /usr/local/bin/zeroclawdash > /var/log/zeroclawdash.log 2>&1 &
```

查看日志：
```bash
tail -f /var/log/zeroclawdash.log
```

### 5. 查看进程

确认服务正在运行：

```bash
ps aux | grep zeroclawdash
```

## 访问 Web 界面

在浏览器中访问：
```
http://<玩客云IP>:42611
```

## API 接口说明

| 接口 | 方法 | 说明 |
|------|------|------|
| `/` | GET | Web 界面 |
| `/api/system/status` | GET | 获取系统状态 |
| `/api/config` | GET/POST | 读取/保存配置 |
| `/api/service/control` | POST | 服务控制（start/stop/restart） |
| `/api/logs` | GET | SSE 日志流 |
| `/api/update` | POST | 版本更新 |

## 配置文件路径

ZeroClaw 配置文件：`~/.zeroclaw/config.toml`

## 注意事项

1. 确保 ZeroClaw 已正确安装并可通过 `zeroclaw` 命令调用
2. 确保 journalctl 服务可用（用于日志流）
3. 确保有足够的权限读取 `/proc/stat` 和 `/proc/meminfo`
4. 更新功能需要网络访问 GitHub API
5. 日志文件默认保存在 `/var/log/zeroclawdash.log`
