# Go 博客后端：本地运行与验收

本目录在原仓库中提供独立 Go 后端，使用 Gin、SQLite 和本地文件存储，兼容原 `blog-vue/blog`（C 端）与 `blog-vue/admin`（B 端）。不依赖原 Java 进程、Redis、MySQL 或消息队列；默认不会启用 SMTP、QQ、微博、OSS 或 IP 地区查询。

## 前置与目录

本次验证环境为 macOS arm64、Go 1.22.5、CGO=1、Node 24.16.0、npm 12.0.2、Python 3.9.6。需要 Go 1.22 或以上、可用的 C 编译器（macOS Command Line Tools / Linux gcc）、Bash、Python 3.9 或以上，以及 Node/npm。

| 路径 | 用途 |
| --- | --- |
| `data/blog.db` | 默认 SQLite 数据库；WAL/SHM 文件由 SQLite 管理 |
| `data/files/` | 默认图片、语音、导出文件和初始资源 |
| `data/initial-admin.txt` | 首次空库生成的管理员用户名与随机密码，权限 0600 |
| `bin/blog-server` | 启动脚本构建的后端 |
| `bin/blog.pid`、`bin/blog.log` | 本目录拥有的进程元数据与运行日志 |
| `.local-frontends/{blog,admin}` | 原前端副本及各自的依赖、构建结果 |

这些运行产物均已被 `blog-go/.gitignore` 排除。初始账号仅在首次空库初始化时创建；重复启动不会覆盖账号、角色配置、文章或文件。

## 启动后端

在本目录运行：

```bash
scripts/start.sh
```

默认后端为 [http://localhost:8888](http://localhost:8888)，监听 `127.0.0.1:8888`。启动脚本构建后检查真实 `/healthz`，并通过 Linux `/proc` 或 macOS 系统 `lsof` 核对监听属于自己启动的 PID。macOS 上只对本次新构建的二进制执行 ad-hoc `codesign --force --sign -` 与验证，解决已实测的 Mach-O 签名问题，不改变系统安全设置。

初始新库用户名为 `localadmin@example.test`，符合原 C 端邮箱格式校验。密码在以下本地文件中读取，不写入文档：

```bash
cat data/initial-admin.txt
```

`.test` 登录名用于本地初始化，不表示该邮箱可实际收信。已有数据库继续使用其既有账号和密码，包括已有的 `local-admin`，不会被启动过程重命名或重置。新密码要求 UTF-8 长度 8–72 字节。

可用环境变量隔离数据或修改监听地址：

```bash
BLOG_DATA_DIR=/absolute/path/to/blog-data \
BLOG_ADDR=127.0.0.1:8888 \
BLOG_PUBLIC_URL=http://localhost:8888 \
BLOG_FRONTEND_URL=http://localhost:8080 \
BLOG_ALLOWED_ORIGINS=http://localhost:8080,http://localhost:8081 \
scripts/start.sh
```

脚本不自动读取 `.env` 或原 Java 配置。可参考 [.env.example](.env.example)，在当前终端显式导出所需变量。原 `application-prod.yml` 不是 Go 配置来源。

## 准备并运行原两端

```bash
python3 scripts/prepare-frontends.py --build
```

脚本复制原源码到 `.local-frontends`，只规范化锁文件中旧淘宝/npmmirror tarball 地址到官方 npm 源；包版本和 integrity 保持不变。旧 `/download/` 地址同时转为 npm 对应的 `/-/` tarball 路径。执行 `npm ci --legacy-peer-deps` 后，使用 `NODE_OPTIONS=--openssl-legacy-provider` 构建。原 Vue 文件和用户原目录中的改动不被覆盖；发现已有副本被编辑或来源变化时，脚本拒绝覆盖，可用新的 `--output` 目录。

分别在两个终端运行：

```bash
python3 scripts/prepare-frontends.py --serve blog
```

```bash
python3 scripts/prepare-frontends.py --serve admin
```

C 端固定为 [http://localhost:8080](http://localhost:8080)，B 端为 [http://localhost:8081](http://localhost:8081)。两端统一使用 `localhost`，不要混用 `127.0.0.1`，以保持 Cookie 域一致。预览服务提供 SPA 路由回退，按原 `^/api → 空` 规则代理至 Go 8888，并保留请求方法、查询、上传字节、Cookie、状态码与响应头；文件 URL 和 WebSocket URL由后端网站配置提供。

生产构建预览解决了原 B 端旧开发服务器在 Node 24 下 `process.binding('http_parser')` 不可用的问题，未更改业务页面或依赖锁定版本。`--serve` 只运行已有 `dist`，不再次复制或安装；按 Ctrl+C 停止各自的预览终端。可通过 `--output` 指向其他已构建副本，通过 `--backend` 指定其他本机 HTTP 后端。`--port` 仅用于隔离测试或显式自定义端口，调整后应同步后端允许的 Origin。

## 停止、重启与备份

```bash
scripts/stop.sh
scripts/start.sh
```

停止脚本核对 PID、启动时间和本目录二进制路径后才发送 SIGTERM，给 HTTP、WebSocket 和数据库 5 秒清理时间。不按端口杀进程，也不对超时进程自动 SIGKILL；无法确认进程归属时会拒绝停止并保留记录。同一目录一次只管理一个后端进程。

并发启停会被锁拒绝。若启停脚本被强制中断而残留 `bin/.start-lock`，先确认没有仍在执行的启停操作，再移除这个空锁目录后重试。

会话保存在内存中，绝对期限为 10 小时，后端重启后需要重新登录。账号、文章、网站资料、评论、点赞、浏览事实和文件持久保存。文章浏览按随机浏览器会话 Cookie 去重；网站访问上报沿用“每日 IP + 浏览器 + 系统”去重口径，两者不是同一统计。

备份时先成功停止后端，再复制整个数据目录，包含数据库、可能存在的 WAL/SHM、`files/` 和初始账号文件。例如使用默认目录：

```bash
scripts/stop.sh
tar -czf /your/backup/location/blog-data.tgz data
```

备份含账号文件与业务数据，应按敏感资料保存。恢复时在进程停止状态下恢复完整目录，并用相同 `BLOG_DATA_DIR` 启动。修改对外 URL 后，已有内容里的绝对文件链接不会被后台自动批量重写。

## 配置键

| 配置键 | 默认或要求 |
| --- | --- |
| `BLOG_ADDR` | `127.0.0.1:8888`；启动脚本要求明确的非零端口 |
| `BLOG_DATA_DIR` | `./data`，相对本目录 |
| `BLOG_PUBLIC_URL` | `http://localhost:8888`，用于文件与 WebSocket 地址 |
| `BLOG_FRONTEND_URL` | `http://localhost:8080` |
| `BLOG_ALLOWED_ORIGINS` | `http://localhost:8080,http://localhost:8081` |
| `BLOG_TIMEZONE` | `Asia/Shanghai` |
| `BLOG_UPLOAD_MODE` | `local`；显式选择 `oss` 才使用 OSS |
| `BLOG_OSS_ENDPOINT`、`BLOG_OSS_REGION`、`BLOG_OSS_BUCKET` | OSS 接入点、区域与桶；HTTPS |
| `BLOG_OSS_PUBLIC_URL` | 实际可读取对象的 HTTPS 地址前缀 |
| `BLOG_OSS_ACCESS_KEY_ID`、`BLOG_OSS_ACCESS_KEY_SECRET` | 外部凭据，无默认值 |
| `BLOG_SMTP_HOST`、`BLOG_SMTP_PORT` | SMTP 服务地址、端口 |
| `BLOG_SMTP_TLS_MODE` | `tls` 或 `starttls`，不使用明文 SMTP |
| `BLOG_SMTP_USERNAME`、`BLOG_SMTP_PASSWORD` | 配对填写认证信息，或按服务配置同时留空 |
| `BLOG_SMTP_FROM` | 合法发件邮箱 |
| `BLOG_QQ_APP_ID` | 与实际 QQ 授权应用一致 |
| `BLOG_WEIBO_APP_KEY`、`BLOG_WEIBO_APP_SECRET`、`BLOG_WEIBO_REDIRECT_URI` | 与实际微博应用及回调一致 |
| `BLOG_IP_ENABLED` | `false`；显式 `true` 才查询地区 |
| `BLOG_IP_ENDPOINT` | 启用后提供原接口兼容的地区查询地址 |

SMTP、QQ、微博、OSS 缺配置时返回真实失败，不使用演示成功。验证码只有 SMTP 明确接受并记录结果后才激活；接受不等于收件箱送达。实际 OAuth 联调还要求应用与前端授权入口/回调匹配。本次外部服务的受控验证和真实外部状态分别记录在 [本地验收记录](docs/local-verification.md)。

## 自动化验证

```bash
scripts/verify.sh
```

脚本清除其子进程中的 `BLOG_*` 环境配置，使用独立临时数据库/文件与本地受控提供方；依次执行禁止内联、禁测试缓存的全包测试、全包 race、vet 和构建。构建输出在 `bin/verify`，不会覆盖正在运行的 `bin/blog-server`。

`test/integration` 的主要入口：

- `TestAllBusinessRoutes`：108 个业务路由的真实 Gin 请求、状态与数据读回。
- `TestAccessBoundaries`：未登录/受限身份矩阵、数据无变更、权限遗漏和作者 ID。
- `TestPersistentRestart`：隔离副本中的真实构建、启动、停止、重启、会话失效及文件引用。
- `TestTwoClients`：真实双 WebSocket、WAV 读回、撤回、人数和重连历史。
- 其他集成测试覆盖受控 SMTP/OAuth/OSS、验证码并发/过期、前端副本/代理，以及真实页面发现的兼容回归。

自动接口测试不能替代浏览器页面验收。逐 FR 的实际页面、网络和运行证据见 [docs/local-verification.md](docs/local-verification.md)。本次交付验证针对本机单实例，不包含容量承诺。
