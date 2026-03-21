# ZeroClawDash 部署指南

## 编译说明

### 本地编译（开发测试）
```bash
make build
```

### 交叉编译（玩客云 ARMv7）
```bash
make build-armv7
```

### 编译所有版本
```bash
make build-all
```

## 部署步骤

### 1. 上传二进制文件
将编译好的 `zeroclawdash-armv7` 文件上传到玩客云设备

### 2. 安装到系统路径
```bash
chmod +x zeroclawdash-armv7
sudo cp zeroclawdash-armv7 /usr/local/bin/zeroclawdash
```

### 3. 创建 systemd 服务文件
```bash
sudo nano /etc/systemd/system/zeroclawdash.service
```

添加以下内容：
```ini
[Unit]
Description=ZeroClawDash Web Manager
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/zeroclawdash
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

### 4. 启动服务
```bash
sudo systemctl daemon-reload
sudo systemctl enable zeroclawdash
sudo systemctl start zeroclawdash
```

### 5. 查看服务状态
```bash
sudo systemctl status zeroclawdash
```

## 访问 Web 界面

在浏览器中访问：
```
http://<玩客云IP>:8080
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
