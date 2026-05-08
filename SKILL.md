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

### 2. 生成 TLS 证书（Cowork 要求 HTTPS）

检查是否已有证书文件。如果没有，使用 mkcert 生成：

```bash
# 检测 mkcert
which mkcert >/dev/null 2>&1 || {
  echo "请先安装 mkcert: brew install mkcert (macOS) / scoop install mkcert (Windows) / 下载 https://github.com/FiloSottile/mkcert/releases"
  exit 1
}

mkcert -install    # 首次执行，安装本地 CA
mkcert localhost   # 生成 localhost.pem 和 localhost-key.pem
```

### 3. 生成配置

读取 `config.example.yaml`，询问用户以下关键信息：

| 字段 | 说明 |
|------|------|
| `upstream_base_url` | 上游 API 地址 |
| `target_model` | force 模式的目标模型 |
| `mode` | force / prefix / 留空(透明转发) |
| `mock_models` | 要追加到 /v1/models 的模型列表 |
| `tls_cert` / `tls_key` | 证书路径（mkcert 生成的路径） |

生成 `config.yaml`。

### 4. 启动

```bash
./proxy -config config.yaml   # Windows: .\proxy.exe -config config.yaml
```

服务监听在 `https://127.0.0.1:18080/apps/anthropic`

### 5. 验证

```bash
curl -sk https://localhost:18080/apps/anthropic/v1/models
```

应返回上游原始模型列表 + mock_models 追加的内容。

### 6. 告知用户 Cowork 配置

```
Base URL:  https://localhost:18080/apps/anthropic
API Key:   用户的上游 API Key
Model:     与 mock_models 中任一项匹配
```
