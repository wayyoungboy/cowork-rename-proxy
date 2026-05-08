# cowork-rename-proxy

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

HTTP 反向代理，拦截 Anthropic 格式请求，将客户端传入的模型名强制替换为指定的上游模型，并支持 SSE 流式转发。

## 解决的问题

Cowork 等客户端只接受 `claude-` 开头的模型名，但上游可能只支持特定模型名（如 `glm-5.1`）。代理在中间做双向改写：

- **请求**：客户端发任意模型 → 改写 → 转发上游
- **响应**：上游返回模型 → 改写回 `claude-` 前缀 → 返回客户端
- **模型列表**：`/v1/models` 将上游原始列表 + mock_models 追加后返回

## 快速开始

### 手动部署

```bash
cp config.example.yaml config.yaml
vim config.yaml          # 编辑上游地址和模型
./proxy
```

### Agent 部署

如果你的终端支持 AI Agent（如 Claude Code），输入：

```
/deploy
```

Agent 会自动编译、生成 TLS 证书、引导配置并启动服务。

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
host: "0.0.0.0"                          # 监听地址
port: 18080                              # 监听端口
tls: true                                # 开启 HTTPS
tls_cert: ""                             # 证书路径（推荐 mkcert 生成）
tls_key:  ""                             # 私钥路径（推荐 mkcert 生成）
upstream_base_url: ""                    # 上游地址，必填
mode: "force"                            # force / prefix / ""(透明转发)
target_model: "glm-5.1"                  # force 模式强制使用的模型
model_prefix: "claude-"                  # prefix 模式添加/去除的前缀
mock_models:                             # 追加到 /v1/models 的模型列表
  - "claude-glm-5.1"
  - "claude-sonnet-4-6"
  - "claude-sonnet-4.6"
```

## 工作模式

| 模式 | 配置 | 行为 |
|------|------|------|
| **force**（统一代理） | `mode: "force"` + `target_model` | 所有请求强制改写为 target_model |
| **prefix**（前缀映射） | `mode: "prefix"` + `model_prefix` | `claude-X` ↔ `X` 双向改写 |
| 透明转发 | `mode: ""`（留空） | 不改写任何模型，直接代理到上游，不拦截 `/v1/models` |

## 启动

```bash
# 默认 config.yaml
./proxy

# 指定配置文件
./proxy -config config-upstream.yaml

# CLI 参数覆盖
./proxy -host 127.0.0.1 -port 8080 -target_model glm-5.1

# 指定证书
./proxy -tls_cert /etc/ssl/cert.pem -tls_key /etc/ssl/key.pem
```

## 热更新

修改 `config.yaml` 后无需重启，代理每 2 秒自动检测并重新加载。

```bash
vim config.yaml   # 编辑配置
# 2 秒后自动生效，日志打印 [config] reloaded
```

> **注意**：`host` / `port` / `tls` 字段修改后需重启才能生效。

## 使用场景

### macOS 本机开发

1. 生成受信 TLS 证书：
   ```bash
   brew install mkcert
   mkcert -install          # 安装本地 CA（仅首次）
   mkcert localhost         # 生成 localhost.pem / localhost-key.pem
   ```

2. 配置 `config.yaml`：
   ```yaml
   host: "0.0.0.0"
   port: 18080
   tls: true
   tls_cert: "localhost.pem"
   tls_key: "localhost-key.pem"
   upstream_base_url: "https://coding.dashscope.aliyuncs.com/apps/anthropic"
   mode: "force"
   target_model: "glm-5.1"
   model_prefix: "claude-"
   mock_models:
     - "claude-glm-5.1"
     - "claude-sonnet-4-6"
     - "claude-sonnet-4.6"
   ```

3. 启动并配置 Cowork：
   ```
   Base URL:  https://localhost:18080/apps/anthropic
   API Key:   你的上游 API Key
   Model:     claude-glm-5.1
   ```

### Windows 本机开发

1. 安装 mkcert（管理员 PowerShell）：
   ```powershell
   scoop install mkcert
   mkcert -install
   mkcert localhost
   ```

2. 编译或下载预编译二进制：
   ```bash
   GOOS=windows GOARCH=amd64 go build -o proxy.exe .
   ```

3. 配置同 macOS（证书路径用相对路径或 Windows 绝对路径）。

4. 启动：
   ```powershell
   .\proxy.exe
   ```

5. 开机自启（可选）：
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

代理在一台机器上运行，Cowork 在其他设备通过局域网访问。

1. 生成本机局域网 IP 证书（假设 `192.168.1.100`）：
   ```bash
   mkcert 192.168.1.100
   ```

2. Cowork 配置：
   ```
   Base URL:  https://192.168.1.100:18080/apps/anthropic
   API Key:   你的上游 API Key
   Model:     claude-glm-5.1
   ```

   **注意**：Cowork 所在设备必须信任 mkcert 的本地 CA。macOS/Windows 自动信任；Linux 需要 `mkcert -install`；iOS/Android 需手动导入 root CA（`mkcert -CAROOT` 找到证书路径）。

### Linux 服务器部署（生产环境）

1. 编译并上传：
   ```bash
   GOOS=linux GOARCH=amd64 go build -o proxy-linux .
   scp proxy-linux user@your-server:/opt/cowork-proxy/
   scp your-domain_*.pem user@your-server:/opt/cowork-proxy/
   ```

2. 服务器配置：
   ```yaml
   host: "0.0.0.0"
   port: 443
   tls: true
   tls_cert: "/opt/cowork-proxy/your-domain_certificate.pem"
   tls_key: "/opt/cowork-proxy/your-domain_private.key"
   upstream_base_url: "https://coding.dashscope.aliyuncs.com/apps/anthropic"
   mode: "force"
   target_model: "glm-5.1"
   mock_models:
     - "claude-glm-5.1"
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

4. 防火墙：
   ```bash
   ufw allow 443/tcp      # Ubuntu
   firewall-cmd --add-port=443/tcp --permanent && firewall-cmd --reload   # CentOS
   ```

5. Cowork 配置：
   ```
   Base URL:  https://your-domain.com/apps/anthropic
   ```

## 支持的端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/apps/anthropic/v1/models` | 上游原始列表 + mock_models 追加 |
| HEAD | `/apps/anthropic/v1/*` | 探活检查 |
| POST | `/apps/anthropic/v1/messages` | Anthropic 消息接口（支持 SSE 流式） |
| POST | `/apps/anthropic/v1/chat/completions` | OpenAI 兼容接口 |

## License

Apache 2.0 — see [LICENSE](LICENSE)
