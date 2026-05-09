# cowork-rename-proxy

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

轻量级本地 API 代理/路由，支持多上游 Provider 切换。将 Anthropic/OpenAI 格式请求转发到配置的模型服务器。

## 解决的问题

Cowork、Claude Code 等客户端只接受特定模型名或 API Key 格式。代理在中间做：

- **Provider 切换**：配置多个上游，通过 `current_provider` 选择当前目标
- **模型改写**：请求/响应中的 model 名双向改写
- **API Key 注入**：每个 Provider 可配置独立 Key
- **多客户端支持**：标准路径 `/v1/*`（Claude Code）和 Cowork 路径 `/apps/anthropic/v1/*`

## 快速开始

```bash
cp config.example.yaml config.yaml
vim config.yaml          # 编辑 upstream 和 models
./proxy
```

## 编译

```bash
go mod tidy
go build -o proxy .

# 跨平台编译
GOOS=darwin  GOARCH=arm64 go build -o proxy-darwin-arm64 .   # Mac Apple Silicon
GOOS=darwin  GOARCH=amd64  go build -o proxy-darwin-amd64 .   # Mac Intel
GOOS=linux   GOARCH=amd64  go build -o proxy-linux .           # Linux
```

零第三方依赖（仅 `gopkg.in/yaml.v3`）。

## 配置

```yaml
host: "0.0.0.0"
port: 18080
tls: true
tls_cert: "localhost.pem"
tls_key: "localhost-key.pem"
current_provider: "dashscope"

providers:
  - name: "dashscope"
    base_url: "https://coding.dashscope.aliyuncs.com/apps/anthropic"
    api_key: ""
    mode: "force"
    target_model: "glm-5.1"
    model_prefix: "claude-"
    models:
      - "glm-5.1"
      - "claude-glm-5.1"

  - name: "openrouter"
    base_url: "https://openrouter.ai/api/v1"
    api_key: "sk-or-xxx"
    models:
      - "claude-sonnet-4-6"
      - "claude-sonnet-4.6"

mock_models:
  - "claude-sonnet-4-6"
  - "claude-sonnet-4.6"
```

### 配置字段

| 字段 | 说明 |
|------|------|
| `current_provider` | 当前激活的 provider name，必须在 providers 列表中 |
| `providers[].name` | provider 唯一标识 |
| `providers[].base_url` | 上游 API 地址 |
| `providers[].api_key` | 可选，有则注入/覆盖客户端的 key |
| `providers[].models` | 支持的模型列表，空=全部接受 |
| `providers[].mode` | `force`/`prefix`/`""`(透明) |
| `providers[].target_model` | force 模式目标 |
| `providers[].model_prefix` | prefix 模式前缀 |
| `mock_models` | 追加到 `/v1/models` 响应的额外模型列表 |

## Provider 工作模式

| 模式 | 行为 |
|------|------|
| **force** | 请求 model → target_model，响应 model → target_model |
| **prefix** | 请求 model → 去除 prefix 转发，响应 model → 加 prefix 返回 |
| 透明 `""` | 不改写任何 model，直接代理 |

## 切换 Provider

修改配置文件中 `current_provider`，2 秒后自动热重载生效：

```bash
vim config.yaml   # 修改 current_provider
# 2 秒后日志打印 [config] reloaded
```

也可用 CLI 参数覆盖：

```bash
./proxy -provider dashscope
```

## 启动

```bash
# 默认 config.yaml
./proxy

# 指定配置文件
./proxy -config config-upstream.yaml

# CLI 参数覆盖
./proxy -host 127.0.0.1 -port 8080 -provider dashscope

# 指定证书
./proxy -tls_cert /etc/ssl/cert.pem -tls_key /etc/ssl/key.pem
```

## 支持的端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/models` | 聚合所有 provider models + mock_models |
| POST | `/v1/messages` | Anthropic 消息接口（支持 SSE 流式） |
| POST | `/v1/chat/completions` | OpenAI 兼容接口 |
| GET | `/apps/anthropic/v1/models` | 同上（Cowork 兼容路径） |
| POST | `/apps/anthropic/v1/messages` | 同上（Cowork 兼容路径） |
| HEAD | `/apps/anthropic/v1/*` | 探活检查 |

## 使用场景

### macOS 本机开发

1. 生成受信 TLS 证书：
   ```bash
   brew install mkcert
   mkcert -install
   mkcert localhost
   ```

2. 配置 `config.yaml` 并启动：
   ```bash
   ./proxy
   ```

3. 客户端配置：
   ```
   # Claude Code
   ANTHROPIC_BASE_URL: https://localhost:18080
   ANTHROPIC_MODEL: claude-sonnet-4-6

   # Cowork
   Base URL:  https://localhost:18080/apps/anthropic
   API Key:   你的 API Key
   Model:     claude-sonnet-4-6
   ```

### Windows 本机开发

1. 安装 mkcert（管理员 PowerShell）：
   ```powershell
   scoop install mkcert
   mkcert -install
   mkcert localhost
   ```

2. 编译或下载预编译二进制后启动：
   ```powershell
   .\proxy.exe
   ```

3. 开机自启（可选）：
   ```powershell
   $action = New-ScheduledTaskAction -Execute "C:\cowork-proxy\proxy.exe" -WorkingDirectory "C:\cowork-proxy"
   $trigger = New-ScheduledTaskTrigger -AtLogOn
   Register-ScheduledTask -TaskName "CoworkProxy" -Action $action -Trigger $trigger -RunLevel Highest
   ```

### Linux 桌面

1. 安装 mkcert：
   ```bash
   sudo apt install -y libnss3-tools
   wget https://github.com/FiloSottile/mkcert/releases/download/v1.4.4/mkcert-v1.4.4-linux-amd64
   sudo mv mkcert-v1.4.4-linux-amd64 /usr/local/bin/mkcert
   sudo chmod +x /usr/local/bin/mkcert
   mkcert -install && mkcert localhost
   ```

2. 编译与启动：
   ```bash
   go build -o proxy . && ./proxy
   ```

### 局域网访问

代理在一台机器上运行，客户端在其他设备通过局域网访问。

1. 生成本机局域网 IP 证书（假设 `192.168.1.100`）：
   ```bash
   mkcert 192.168.1.100
   ```

2. 客户端配置：
   ```
   Base URL:  https://192.168.1.100:18080
   ```

   **注意**：客户端设备必须信任 mkcert 本地 CA。macOS/Windows 自动信任；Linux 需要 `mkcert -install`；iOS/Android 需手动导入 root CA。

### Linux 服务器部署（生产环境）

1. 编译并上传：
   ```bash
   GOOS=linux GOARCH=amd64 go build -o proxy-linux .
   scp proxy-linux user@your-server:/opt/cowork-proxy/
   scp your-domain_*.pem user@your-server:/opt/cowork-proxy/
   ```

2. 服务器配置（使用真实域名证书）：
   ```yaml
   host: "0.0.0.0"
   port: 443
   tls: true
   tls_cert: "/opt/cowork-proxy/your-domain_certificate.pem"
   tls_key: "/opt/cowork-proxy/your-domain_private.key"
   current_provider: "dashscope"
   providers:
     - name: "dashscope"
       base_url: "https://coding.dashscope.aliyuncs.com/apps/anthropic"
       mode: "force"
       target_model: "glm-5.1"
       model_prefix: "claude-"
       models:
         - "glm-5.1"
         - "claude-glm-5.1"
   mock_models:
     - "claude-sonnet-4-6"
     - "claude-sonnet-4.6"
   ```

3. systemd 守护：
   ```ini
   [Unit]
   Description=Cowork Rename Proxy
   After=network.target

   [Service]
   ExecStart=/opt/cowork-proxy/proxy-linux -config /opt/cowork-proxy/config.yaml
   Restart=always

   [Install]
   WantedBy=multi-user.target
   ```
   ```bash
   systemctl daemon-reload
   systemctl enable cowork-proxy
   systemctl start cowork-proxy
   ```

## 热更新

修改 `config.yaml` 后无需重启，代理每 2 秒自动检测并重新加载。

> **注意**：`host` / `port` / `tls` 字段修改后需重启才能生效。

## License

Apache 2.0 — see [LICENSE](LICENSE)
