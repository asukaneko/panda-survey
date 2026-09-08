# 熊猫问卷

私有部署的轻量在线问卷调查系统。Go 后端 + 原生 HTML/CSS/JS 前端 + SQLite，单二进制交付，手机与电脑同一套页面自适应。

## 功能

- 多用户注册登录（bcrypt + Cookie Session，首个注册用户自动成为管理员）
- 管理员后台（/admin）：全站用量总览、用户封禁/解封、问卷强制下架/删除
- 问卷管理：创建、卡片式编辑、九种题型（单选 / 多选 / 填空 / 简答 / 下拉 / 评分 / 日期 / 矩阵 / 排序）、必答设置、选项管理、预览、复制、软删除
- 逻辑跳转：题目可设显示条件（依赖前面单选/下拉题的选项），填答端实时显隐
- 问卷分页：题目级「此后分页」标记，填答端逐页作答与校验
- 模板库：内置 5 套常用问卷模板，一键创建草稿
- 发布控制：状态机（草稿 / 发布中 / 已停止）、回收截止时间、回收量上限、自动停止
- 保存采用乐观锁（updated_at 版本比对），并发编辑不会静默互相覆盖；问卷在任意状态（草稿 / 发布中 / 已停止）均可直接编辑保存，保存时按题目 ID 原地更新，历史答卷的统计不受影响；也可用「复制」生成新草稿另改一份
- 填答端：匿名链接 + 二维码、必答校验定位、感谢页、按 IP 限频
- 统计：逐题图表（Chart.js 本地化）、矩阵逐行分布、排序平均名次、评分均值、开放题列表、单份答卷删除、明细与汇总 CSV 导出（UTF-8 BOM，Excel 直接打开）
- 统计协作：只读分享令牌，/share/{token} 匿名查看统计图表
- AI 能力（Agent）：管理员配置任意 OpenAI 兼容服务后，用户可「AI 创建问卷」「自然语言对话改题」「单题 AI 优化」「开放题 AI 摘要」；AI 修改以提案呈现，用户确认后仍走手动保存，AI 无直接写库路径；每用户每日配额可调

## 快速开始

要求 Go 1.22+。

```bash
go run .            # http://localhost:43210，当前目录生成 panda.db
```

打开浏览器注册第一个账号（自动成为管理员）即可使用。

## 编译部署

```bash
# 三平台编译（无 CGO，纯 Go）
GOOS=linux   GOARCH=amd64 go build -o dist/panda-survey-linux .
GOOS=windows GOARCH=amd64 go build -o dist/panda-survey.exe .
GOOS=darwin  GOARCH=arm64 go build -o dist/panda-survey-mac .

# 服务器上：复制单文件即可运行
./panda-survey-linux
```

静态资源已通过 go:embed 打进二进制，无需附带 web 目录。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `PORT` | 43210 | 监听端口 |
| `DB_PATH` | ./panda.db | SQLite 文件路径 |
| `SESSION_TTL` | 168h | 会话有效期 |
| `BASE_URL` | 空 | 分享链接前缀（反代场景） |
| `SECRET_KEY` | 空 | AI API Key 加密密钥（任意非空字符串；不设则明文存储并在设置页提示） |
| `ADMIN_USERNAMES` | 空 | 逗号分隔，指定管理员用户名 |

## AI 配置

1. 用管理员账号进入「设置」页（/admin/settings）
2. 填写任意 OpenAI 兼容服务：Base URL（到 /v1 一级）、API Key、模型名，点「测试连接」验证
3. 保存后全体用户可用：控制台「AI 创建」、编辑页「AI 面板」（对话式改题）、题目卡片「AI 优化」

建议设置 `SECRET_KEY` 环境变量，API Key 将以 AES-GCM 加密存储。

## 备份

```bash
# cron 每日备份，保留最近 30 份
0 3 * * * /opt/panda-survey/scripts/backup.sh /opt/panda-survey
```

## 开发

```bash
go vet ./...     # 静态检查
go test ./...    # 全量测试（题型 / 认证 / AI mock / API 集成测试）

# 造数据验证统计正确性
go run .                                   # 终端 1：启动服务
go run ./scripts/seed.go -survey 1 -n 100  # 终端 2：批量提交并打印分布计数
```

目录结构：

```
panda-survey/
├── main.go                 # 入口：配置、迁移、路由、自动停止巡检
├── internal/
│   ├── config/             # 环境变量
│   ├── db/                 # SQLite 连接与 schema_version 迁移
│   ├── model/              # 数据结构
│   ├── auth/               # bcrypt / Session / 登录锁定
│   ├── middleware/         # 日志、恢复、限频、CSRF、认证守卫
│   ├── registry/           # 题型注册中心
│   ├── questiontype/       # 六种题型适配器 + 表驱动测试
│   ├── store/              # SQL 仓储
│   ├── service/            # 业务：保存校验 / 提交校验 / 统计 / CSV
│   ├── ai/                 # OpenAI 兼容客户端、生成、Agent 工具循环
│   └── handler/            # HTTP 处理器 + 集成测试
├── web/                    # go:embed 内嵌前端（原生 HTML/CSS/JS）
└── scripts/                # 造数据与备份脚本
```

## 安全设计

- 用户输入一律经 `textContent` / `createElement` 渲染，禁拼 innerHTML（XSS）
- 写接口仅接受 `application/json` 并校验 Origin 同源（CSRF）
- 密码 bcrypt 哈希；连续登录失败 5 次锁定 10 分钟；修改密码吊销全部会话
- AI 仅作用于本人问卷的内存工作副本，提案确认后才可保存

## 浏览器支持

支持近两年版本的 Chrome / Edge / Firefox / Safari 及微信内置浏览器；不支持 IE。
