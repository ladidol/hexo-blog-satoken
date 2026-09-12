<p align=center>
  <a href="http://blog.ladidol.top">
    <img src="https://figurebed-ladidol.oss-cn-chengdu.aliyuncs.com/img/202212280133505.png" alt="小小的个人博客" style="border-radius: 50%">
  </a>
</p>



<p align="center">
   <a target="_blank" href="https://github.com/ladidol/hexo-blog-satoken">
      <img src="https://img.shields.io/hexpm/l/plug.svg"/>
      <img src="https://img.shields.io/badge/Go-1.22+-00ADD8.svg"/>
      <img src="https://img.shields.io/badge/Gin-1.10.1-00ADD8.svg"/>
      <img src="https://img.shields.io/badge/SQLite-003B57.svg"/>
      <img src="https://img.shields.io/badge/JDK-1.8+-green.svg"/>
      <img src="https://img.shields.io/badge/springboot-2.4.2.RELEASE-green"/>
      <img src="https://img.shields.io/badge/vue-2.5.17-green"/>
      <img src="https://img.shields.io/badge/mysql-8.0.20-green"/>
      <img src="https://img.shields.io/badge/mybatis--plus-3.4.0-green"/>
      <img src="https://img.shields.io/badge/redis-6.0.5-green"/>
      <img src="https://img.shields.io/badge/elasticsearch-7.9.2-green"/>
      <img src="https://img.shields.io/badge/rabbitmq-3.8.5-green"/>
   </a>
</p>



## 项目介绍

项目现提供 **Java 和 Go 两套后端**，共用原 Vue 博客读者端（C 端）与管理端（B 端）。新增的 Go 版本使用 **Gin + database/sql + SQLite**，兼容原前端接口，默认使用本地数据库和文件存储。

| 后端版本 | 目录 | 技术栈与运行方式 |
| --- | --- | --- |
| Java 原版 | [blog-springboot](blog-springboot/) | Spring Boot + MyBatisPlus + MySQL，配套服务按原配置部署 |
| Go 新版 | [blog-go](blog-go/README.md) | Gin + SQLite，单进程本地运行，无需启动 Java、MySQL、Redis 或消息队列 |

本地使用时选择一套后端，并先启动后端，再启动前端；前端的网站配置由后端动态加载。Go 版本首次启动会创建独立 SQLite 数据库和初始内容，不会自动导入原 MySQL 数据。

## 项目目录

```text
hexo-blog-satoken
├── blog-go          -- Go 后端、启停脚本与本地运行文档
├── blog-springboot  -- Java 后端
└── blog-vue
    ├── blog         -- 博客读者端（C 端）
    └── admin        -- 后台管理端（B 端）
```

## Go 版本本地运行

需要 Go 1.22 或以上、C 编译器（macOS Command Line Tools / Linux gcc）、Bash、Python 3.9 或以上，以及 Node/npm。本次已验证 macOS arm64、Go 1.22.5、Node 24.16.0。

从仓库根目录进入 Go 模块并启动后端：

```bash
cd blog-go
scripts/start.sh
```

首次空库初始化的管理员用户名为 `localadmin@example.test`，随机密码保存在 `data/initial-admin.txt`。在 `blog-go` 目录读取：

```bash
cat data/initial-admin.txt
```

准备并构建原 C/B 端的独立副本，原前端源码保持不变：

```bash
python3 scripts/prepare-frontends.py --build
```

分别打开两个终端，均进入仓库的 `blog-go` 目录，运行对应预览服务：

```bash
# 终端一：博客 C 端
python3 scripts/prepare-frontends.py --serve blog
```

```bash
# 终端二：管理 B 端
python3 scripts/prepare-frontends.py --serve admin
```

| 服务 | 本地地址 |
| --- | --- |
| 博客 C 端 | [http://localhost:8080](http://localhost:8080) |
| 管理 B 端 | [http://localhost:8081](http://localhost:8081) |
| Go 后端健康检查 | [http://localhost:8888/healthz](http://localhost:8888/healthz) |

两端统一使用 `localhost`，以保持登录 Cookie 的主机名一致。预览服务把前端 `/api` 请求代理到 Go 后端；采用生产构建预览，兼容原 B 端旧开发服务器在 Node 24 下的 `http_parser` 问题。

停止后端或执行自动化验证，也在 `blog-go` 目录运行：

```bash
scripts/stop.sh
scripts/verify.sh
```

前端预览在各自终端按 `Ctrl+C` 停止。后端数据保存在 `data/`，正常停止与重启会保留内容，重启后需要重新登录。

Go 版本通过 `BLOG_*` 环境变量配置，不自动读取 `.env` 或原 Java 的 `application-prod.yml`。SMTP、QQ/微博、OSS 等外部服务默认未启用，需要单独配置；本次仅完成受控测试，真实外部服务尚未联调。

完整配置、启停、备份和验证说明见 [Go 后端运行文档](blog-go/README.md)，实际前后端联调结果见 [本地验收记录](blog-go/docs/local-verification.md)。

## 原项目演示地址

**项目链接：** [blog.ladidol.top](http://blog.ladidol.top)

**后台链接：** [admin.ladidol.top](http://admin.ladidol.top)

测试账号：test@qq.com，密码：123456，可登入后台查看。

**Github地址：**[ladidol/hexo-blog-satoken (github.com)](https://github.com/ladidol/hexo-blog-satoken)



## 技术介绍

**前端：** vue + vuex + vue-router + axios + vuetify + element + echarts

**Java 后端：** SpringBoot + nginx + docker + SpringSecurity + Swagger2 + MyBatisPlus + Mysql + Redis + elasticsearch + RabbitMQ + MaxWell + Websocket

**Go 后端：** Gin + database/sql + SQLite + gorilla/websocket，本地文件存储及可选 OSS、SMTP、QQ/微博适配。

**Java 版本其他集成：** 接入QQ，微博第三方登录，接入腾讯云人机验证、websocket





## Java 后端目录

```
blog-springboot
├── annotation    --  自定义注解
├── aspect        --  aop模块
├── config        --  配置模块
├── constant      --  常量模块
├── consumer      --  MQ消费者模块
├── controller    --  控制器模块
├── mapper        --  框架核心模块
├── dto           --  dto模块
├── enums         --  枚举模块
├── exception     --  自定义异常模块
├── handler       --  处理器模块（一些自定义处理器）
├── service       --  服务模块
├── strategy      --  策略模块（用于扩展第三方登录，搜索模式，上传文件模式等策略）
├── util          --  工具类模块
└── vo            --  vo模块
```

## 项目特点

- 前台参考"Hexo"的"Butterfly"设计，美观简洁，响应式体验好。
- 后台参考"element-admin"设计，侧边栏，历史标签，面包屑自动生成。
- 采用Markdown编辑器，写法简单。
- 评论支持表情输入回复等，样式参考Valine。
- 添加音乐播放器，支持在线搜索歌曲。
- 前后端分离部署，适应当前潮流。
- 接入第三方登录，减少注册成本。
- 支持发布说说，随时分享趣事。
- 留言采用弹幕墙，更加炫酷。
- 支持代码高亮和复制，图片预览，深色模式等功能，提升用户体验。
- 搜索文章支持高亮分词，响应速度快。
- 新增文章目录、推荐文章等功能，优化用户体验。
- 新增在线聊天室，支持撤回、语音输入、统计未读数量等功能。
- Java 版本通过 AOP 注解实现日志管理，Go 版本通过路由审计记录操作日志。
- 支持动态权限修改，采用RBAC模型，前端菜单和后台权限实时更新。
- 后台管理支持修改背景图片，博客配置等信息，操作简单，支持上传相册。
- Java 版本支持 Elasticsearch / MySQL 搜索模式；Go 版本使用 SQLite 完成文章检索。两套后端均提供本地文件与 OSS 上传适配。
- 代码遵循阿里巴巴开发规范，利于开发者学习。
