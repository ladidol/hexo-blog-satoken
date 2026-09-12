# 本地交付验收记录

日期：2026-09-12，Asia/Shanghai。代码在 `blog-backend-go` 需求的全部模块集成后验证；本记录区分自动测试、本次真实浏览器操作和真实外部服务状态。

版本范围：基线提交 `592764b580728dc508986a85f612fd8d5c34c08c`，叠加本文列出的 TASK-015 交付与兼容修复；最终提交标识由本次 Git/Trek 集成记录提供。

## 环境与证据位置

| 项目 | 实际环境 |
| --- | --- |
| 后端工具链 | macOS arm64，Go 1.22.5，CGO=1 |
| 前端工具链 | Node 24.16.0，npm 12.0.2，`NODE_OPTIONS=--openssl-legacy-provider` |
| 脚本环境 | Python 3.9.6、Bash；新 Mach-O 二进制 ad-hoc codesign 后执行 |
| 地址 | Go `http://localhost:8888`；C `http://localhost:8080`；B `http://localhost:8081` |
| 原发布/图片验收数据 | `/Users/xiaoxiao/IdeaProjects/trek-projects/.trek-tools/blog-backend-go/qa-data` |
| 新初始账号/C 登录验收数据 | `/Users/xiaoxiao/IdeaProjects/trek-projects/.trek-tools/blog-backend-go/qa-data-final` |
| 真实浏览器使用的前端副本 | `/Users/xiaoxiao/IdeaProjects/trek-projects/.trek-tools/blog-backend-go/frontend-qa/blog-vue/{blog,admin}` |
| 前端构建日志 | 上述 `frontend-qa` 目录中的 `blog-build.log`、`admin-build.log` |

两套 QA 目录均为本次专有测试数据，没有读取或复制原 `application-prod.yml`。原 Vue 业务源码没有修改。页面截图已在本次主任务会话中实际查看，本记录不虚构本地截图文件。验收阶段新库后端曾记录为 PID 11594；进程由主任务受控启停，PID 不是长期运行标识。

交付脚本自身还实际执行了：

```bash
python3 scripts/prepare-frontends.py --build
```

两端副本的 `npm ci --legacy-peer-deps` 和生产构建均退出 0，版本与 integrity 保持不变；本次工作区运行日志为 `.local-frontends/prepare.log`。原 B 端开发服务器在 Node 24 下因 `http_parser` 退出，生产构建预览与 API 代理已替代该开发服务用于验收。

## 自动化验证入口与行为

```bash
scripts/verify.sh
```

该入口已实际执行并退出 0：`go test -gcflags='all=-l -N' -count=1 ./...`、对应全包 `-race`、`go vet ./...` 和独立构建全部通过。集成包常规测试为 14.665 秒，race 为 52.084 秒；日志最后输出 `Automated checks passed`。随后补强的监听 PID 核对通过启停专题复验（`TestPersistentRestart`、`TestStopRefusesPIDForDifferentExecutable`、`TestStartRefusesAnAlreadyListeningPort`，5.491 秒）。构建后的两端副本再次通过 `--skip-install` 源码指纹校验。

集成测试的数据目录、动态 localhost 端口和测试身份均独立；受控提供方只允许本地网络。测试日志为本次工作区的 `bin-verification.log`，接口/脚本证据可用上述命令重新产生。

| FR | 自动化覆盖 | 本次真实页面证据 |
| --- | --- | --- |
| FR-001 | 首页/归档/搜索/分类文章匹配和空结果；私密/草稿/删除过滤；详情、点赞取消及浏览事实 | C 端文章 id2 修复后完整显示标题、正文、分类、72 字计数、目录、上一篇和推荐；新库单篇文章 id1 正常显示；点赞 0→1，刷新仍为 1 |
| FR-002 | 草稿编辑、公开/私密切换、置顶、回收站恢复、永久删除与关联清理 | 原 B 端 UI 保存发布文章 id2，列表由 1 条变 2 条，回到 C 端读到相同正文与关联信息；后端重启后可读 |
| FR-003 | 分类/标签/友链创建、修改、搜索、空集合与删除 | B 编辑器实际选择分类/标签后发布；分类“生活记录”与标签“开始”各显示 1 篇，友链“本地博客”可加载；C 详情显示对应关联 |
| FR-004 | 账号与资料 ID 分离、资料/头像读回、错误密码、禁用、改密、退出、重启失效 | B 端登录/动态菜单成功；新空库 `localadmin@example.test` 能通过 C 端邮箱校验并登录；两端共享 localhost Cookie 域 |
| FR-005 | 本地 TLS SMTP 接受/拒绝；注册；可控时钟的过期、找回、绑定和并发消费；QQ/微博受控核验 | 未使用实际邮箱/OAuth 授权材料；不记录真实外部登录或投递成功 |
| FR-006 | 全部受保护路由的未登录/受限矩阵与数据库摘要不变；实时角色资源授权；菜单/角色/资源引用 | B 动态菜单、角色管理、管理员权限树、接口/菜单表均加载；用户列表 1、在线用户 1 |
| FR-007 | 评论/回复/留言审核开关、作者、点赞、级联删除、分页；通知 accepted/failed 如实持久记录 | C 提交评论提示“正在审核中”→B 评论管理显示待审→点击通过→C 刷新显示评论正文与评论数 1 |
| FR-008 | 说说公开/隐藏与点赞取消；相册照片编辑/移动/回收/恢复/永久删除；空相册删除 | B 相册“本地相册”显示 1 张并进入照片“山景”；照片回收站显示暂无照片；说说列表 1 条 |
| FR-009 | 完整网站配置、关于、页面 CRUD 与重启读回 | B 网站设置三标签/表单、11 个页面卡片和关于正文正常显示；C/B 使用后端文件与聊天地址 |
| FR-010 | 同日 IP/浏览器/系统重复上报去重、后台统计、日志查询删除且业务对象不受影响 | B 首页显示访问 1、用户 1、文章 1、留言 0；一周访问图、贡献、文章排行、分类、地域、标签组件可见；日志页能看到审核评论记录 |
| FR-011 | 图片实际解码和 URL 读取、压缩 JPEG 规范化、文章导出字节与标题文件名、导入和失败无残留 | 原 B 端上传 787252 字节 PNG，经旧前端压缩后服务端返回 `.jpg`；页面实际图片 `naturalWidth=512`，文章发布成功 |
| FR-012 | 完整 App 上真实双 WS、文本/WAV 字节读回、归属撤回、人数变化、重连；模块还验证队列与关闭码 | 原 C 端聊天室显示 1 人在线；回车发送“Go 聊天室本地联调成功”后立即显示；整页 reload 后再次打开仍显示历史消息 |
| FR-013 | 原始副本准备、API/SPA 代理、进程归属拒绝、真实构建启停/重启和文件引用保留 | Go+B+C 三进程真实联通；两套数据目录分别保留发布图片/重启证据和新账号/互动证据 |

`TestAllBusinessRoutes` 逐项断言数据状态，并核对 108 项 RouteSpecs 均有实际请求。路由计数仅辅助追溯，不作为业务成功的替代证据。`TestPersistentRestart` 在临时后端副本中真实运行 start/stop 两轮，验证旧会话失效、账号仍能登录、文章/关于/图片引用与浏览事实保留。

## 兼容修复与保留的原行为

- 原 Java `BeanCopyUtils` 在没有上一篇/下一篇时仍创建空 DTO；Go 改为返回三个字段都可空的邻接对象，避免原 Article.vue 直接读取 `.id` 时崩溃。单篇、首/末篇与隐藏邻居均有回归，原 qa-data 文章 id2 的真实页面复查通过。
- 原 C 登录表单只接受邮箱形态；新库初始管理员改为 `localadmin@example.test`，随机密码文件同步。既有数据库不改名、不重置；旧普通账号仍可由后端认证。
- 文章/评论点赞、评论目标、页面及菜单/资源/角色对象的非账号错误按原业务编码返回 51000；52001/52002 保留给账号重复/不存在。只在明确的业务边界修正，并保留底层原因。
- 启动预检允许已停止服务的 TIME_WAIT 端口重用，同时检测真实监听冲突；停止只核对本目录 PID/二进制/启动时间，不按端口杀进程。
- 原 C App.vue 曾在尚未取得网站配置时出现加载阶段瞬态属性错误，首页随后恢复。它与已修复的详情稳定渲染中断分别记录；详情修复并 reload 后，主任务复查没有新增详情 Vue 错误。
- “审核评论”日志的操作类型沿用“删除”：原 Java `CommentController` 为该 PUT 接口标注了 `@OptLog(optType = REMOVE)`，Go 映射与其一致。本次没有将其当成迁移偏差改写，更没有修改已有日志。

除原计划的 11 个交付文件外，验收反馈对应的直接修复仅涉及 `internal/article/service.go`（点赞错误/邻接 DTO）、`internal/community/service.go`（评论业务错误）、`internal/site/service.go`（页面重复错误）、`internal/identity/management.go`（管理对象业务错误）、`internal/bootstrap/seed.go`（新库初始用户名），以及这些契约的回归测试。没有调整表结构、公共构造签名或原 Vue 业务源码。

主任务已在浏览器验收完成后受控停止 C/B 预览和 QA 后端，两套数据目录均保留，以便完成工作区集成；交付后的运行状态应以重新执行启动步骤为准。

## 外部服务事实

| 服务 | 后端受控测试 | 真实外部事实 | 当前配置/状态 |
| --- | --- | --- | --- |
| SMTP | 专有 TLS SMTP 服务接受/拒绝、验证码激活、失败记录与评论通知均覆盖 | 未向真实他人邮箱发信；未验证收件箱送达 | 本机默认未配置；SMTP 接受不等于收件箱送达 |
| QQ / 微博 | 提供方路径、有效/拒绝返回、身份入库与凭据不持久化覆盖 | 未执行真实 OAuth 授权 | 缺应用资料时明确失败，未联调 |
| OSS | 受控 HTTP 提供方验证 V4 请求、上传及返回 URL 的实际字节读取；模块覆盖失败与补偿 | 未访问生产桶或上传真实对象 | 默认 local；OSS 未真实联调 |
| IP 地区 | 本地兼容接口返回受控地区，验证调用与降级 | 未记录真实地区服务可用性 | 默认关闭 |

验证仅证明本机单实例的功能与兼容性，不构成生产容量、真实第三方可用性或投递到达的承诺。
