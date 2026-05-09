---
name: deploy
description: Deploy cowork-rename-proxy — download binaries, configure TLS, generate config, and start the service. Use when the user asks to set up or deploy this proxy.
---

## 部署流程

### 1. 下载预编译二进制

优先引导用户从 [GitHub Releases](https://github.com/wayyoungboy/cowork-rename-proxy/releases) 下载：

| 平台 | 文件 |
|------|------|
| macOS Apple Silicon | `proxy-darwin-arm64` |
| macOS Intel | `proxy-darwin-amd64` |
| Linux AMD64 | `proxy-linux` |
| Windows AMD64 | `proxy.exe` |

下载后设置执行权限：
```bash
chmod +x proxy-*   # macOS / Linux
```

如果 Releases 中没有对应平台，再 fallback 到源码编译：
```bash
GOOS=$(go env GOOS) GOARCH=$(go env GOARCH) go build -o proxy .
```

### 2. 生成 TLS 证书（客户端要求 HTTPS）

检查是否已有证书文件。如果没有，使用 mkcert 生成：

```bash
which mkcert >/dev/null 2>&1 || {
  echo "请先安装 mkcert: brew install mkcert (macOS) / scoop install mkcert (Windows)"
  exit 1
}

mkcert -install    # 首次执行，安装本地 CA
mkcert localhost   # 生成 localhost.pem 和 localhost-key.pem
```

### 3. 生成配置

读取 `config.example.yaml`，询问用户以下关键信息：

| 字段 | 说明 |
|------|------|
| `providers` | 上游列表，每个包含 base_url、api_key（可选）、models 列表、mode |
| `current_provider` | 当前激活的 provider name |
| `mock_models` | 追加到 /v1/models 的模型列表 |
| `tls_cert` / `tls_key` | 证书路径 |

生成 `config.yaml`。

### 4. 启动

```bash
./proxy -config config.yaml   # Windows: .\proxy.exe -config config.yaml
```

服务监听在 `https://127.0.0.1:18080`

### 5. 验证

```bash
curl -sk https://localhost:18080/v1/models
```

应返回所有 provider 的 models + mock_models 的聚合列表。

### 6. 告知用户客户端配置

```
# Claude Code / Cowork
Base URL:  https://localhost:18080
# Cowork 也用 /apps/anthropic 前缀：
Base URL:  https://localhost:18080/apps/anthropic
Model:     与 provider models 列表中任一项匹配
```
