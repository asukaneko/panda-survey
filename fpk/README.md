# 熊猫问卷 fnOS FPK 打包指南

本目录是 fnOS（飞牛私有云）应用包 `panda-survey` 的 **FPK 骨架**（amd64/x86 版；`../fpk-arm/` 为 arm 版）。
按本文档操作即可把当前代码复刻打包成可在 fnOS 应用中心手动安装的 `.fpk` 文件。

---

## 1. FPK 格式速览（已与官方包逐字节核对）

`pandasurvey-0.2.0-x86.fpk`（作者发布于 GitHub Releases）的结构：

```
.fpk = gzip( ustar tar )
        ├── app.tgz            # app 负载的 tar.gz（安装时解到 target/，即 TRIM_APPDEST）
        │      ├── server/panda-survey    # Go 单二进制
        │      ├── ui/config、ui/images/  # 桌面入口配置与图标
        │      └── config/privilege、resource
        ├── LICENSE
        ├── cmd/               # 生命周期脚本：main(start/stop/status) + 8 个 init/callback
        ├── config/privilege   # 运行权限声明（run-as=package）
        ├── config/resource    # 数据共享目录声明
        ├── ICON.PNG / ICON_256.PNG
        ├── manifest           # key=value 元数据，末尾追加 checksum
        └── wizard/uninstall   # 卸载向导（是否保留数据）

细节约定（与官方包一致，脚本已内建）：
- 所有条目 uid=0、gid=0；目录 0777、普通文件 0666；统一 mtime
- manifest 末尾 `checksum = md5(app.tgz)`（对齐方式：key 左对齐 22 列后接 `= `）
- gzip mtime=0；tar 为 ustar（非 pax/gnu）
- app.tgz 内 `config/` 来自骨架根部 `config/`（非 app/ 下），与官方包保持一致
```

> 注：网上流传的「fpk 是 cpio」说法不准确；实测官方包为 **gzip + ustar tar**，首条目即 `app.tgz`。

---

## 2. 前置条件

| 依赖 | 用途 | 备注 |
| --- | --- | --- |
| Go 1.25+（go.mod 要求 `go 1.25.0`） | 编译后端 | 无 CGO，纯 Go，可交叉编译 |
| Python 3 | 运行打包脚本 | 仅用标准库，无第三方依赖 |
| （可选）tar/bsdtar、file | 手工校验产物 | Windows 自带 tar（libarchive），fnOS 同生态 |

---

## 3. 一键打包（推荐）

```bash
# 3.1 交叉编译 linux/amd64 二进制
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/panda-survey-linux .

# 3.2 打包为 fpk（x86 = amd64）
python3 scripts/pack_fpk.py \
  --skeleton fpk --binary dist/panda-survey-linux \
  --version 0.3.0 --platform x86 --out dist

# 3.3 校验产物（结构/顺序/checksum/ELF 架构）
python3 scripts/pack_fpk.py --verify dist/pandasurvey-0.3.0-x86.fpk
```

产物：`dist/pandasurvey-0.3.0-x86.fpk`（arm 版把 `--skeleton` 换成 `fpk-arm`、`--platform` 换成 `arm`）。

打包是**确定性**的：相同输入两次打包产出字节一致的 fpk。

---

## 4. 手动打包流程（脚本做了什么）

不依赖脚本时可按此步骤手工复刻：

```bash
# 1) 组装 app 负载目录，打成 app.tgz
mkdir -p build-app/server build-app/ui/images build-app/config
cp dist/panda-survey-linux        build-app/server/panda-survey
cp fpk/app/ui/config              build-app/ui/config
cp fpk/app/ui/images/icon_64.png  build-app/ui/images/icon_64.png
cp fpk/app/ui/images/icon_256.png build-app/ui/images/icon_256.png
cp fpk/config/privilege           build-app/config/privilege
cp fpk/config/resource            build-app/config/resource
# 注：保留 server/ui/config 三个目录与条目顺序
tar -czf app.tgz -C build-app server ui config

# 2) manifest 追加 checksum（md5 of app.tgz），key 左对齐 22 列
printf 'checksum              = %s\n' "$(md5sum app.tgz | cut -d' ' -f1)" >> manifest

# 3) 组装外层 tar：顺序为 app.tgz → LICENSE → cmd/* → config/* → ICON* → manifest → wizard/*
tar -cf inner.tar app.tgz LICENSE \
    cmd cmd/config_callback cmd/config_init cmd/install_callback cmd/install_init \
    cmd/main cmd/uninstall_callback cmd/uninstall_init cmd/upgrade_callback cmd/upgrade_init \
    config config/privilege config/resource \
    ICON.PNG ICON_256.PNG manifest wizard wizard/uninstall

# 4) gzip 收尾（mtime=0）
gzip -n -c inner.tar > pandasurvey-0.3.0-x86.fpk
```

注意：手工打包时请保持 ustar 格式、目录/文件权限 0777/0666、LF 换行（见第 6 节），
否则与脚本产物不一致。**日常复刻直接用第 3 节的脚本即可。**

---

## 5. 骨架目录说明

| 路径 | 作用 | 修改时机 |
| --- | --- | --- |
| `manifest` | 应用元数据：appname/version/display_name/desc/changelog/platform/service_port 等 | 每次发版：升 version、补 changelog |
| `cmd/main` | start/stop/status 启停脚本；注入 PORT/DB_PATH/SESSION_TTL/APP_VERSION | 一般不动；改环境变量注入时 |
| `cmd/*_init|*_callback` | 安装/升级/卸载/配置生命周期钩子 | 一般不动 |
| `config/privilege` | 权限声明（run-as=package 非 root） | 一般不动 |
| `config/resource` | 数据共享目录声明（问卷数据持久化） | 一般不动 |
| `app/ui/config` | 桌面入口：http://本机:43210/ 打开熊猫问卷 | 一般不动 |
| `app/ui/images/` | 桌面图标 64/256 | 换图标时 |
| `wizard/uninstall` | 卸载时询问是否保留数据 | 一般不动 |
| `ICON.PNG`/`ICON_256.PNG`/`LICENSE` | 应用中心图标与许可 | 一般不动 |

---

## 6. 发版检查清单

- [ ] `manifest`：`version` 升到新版本号，`changelog` 追加该版本说明（`<br>` 分隔，UTF-8）
- [ ] `cmd/main`：`APP_VERSION` 默认值与 manifest 版本一致（关于页显示/更新检测用）
- [ ] 全部脚本与 manifest 为 **LF 换行、UTF-8 编码**（`.gitattributes` 已强制 LF；Windows 编辑注意不要存成 CRLF，否则 fnOS 上 bash 执行/解析报错）
- [ ] 编译与全量测试通过：`go vet ./... && go test ./...`
- [ ] 打包后 `--verify` 全部 PASS
- [ ] 安装冒烟：fnOS 应用中心 → 手动安装 → 启动 → 注册首个管理员 → 建卷/发布/填答
- [ ] （如对外分发）替换 `manifest` 中 maintainer/distributor 为自身信息

---

## 7. 常见问题

**Q：fnOS 上 bash 脚本报错 / manifest 解析失败？**
检查换行：`file cmd/main manifest` 若出现 `CRLF`，用 `git add --renormalize .` 或
`dos2unix` 修正后重打包。

**Q：`--verify` 报 checksum 不匹配？**
manifest 里的 `checksum` 必须与 `app.tgz` 同时更新——用脚本打包会自动处理；手工改过
manifest 后重跑 `--verify` 会失败，重新打包即可。

**Q：产物能直接装到别人的 fnOS 吗？**
能（手动安装）。但 manifest 的 maintainer/来源默认仍是原作者 PanDa（www.aykeji.cn）；
对外分发建议改为自己的信息，并注意不要冒用他人品牌。

**Q：想走作者官网自动更新通道？**
做不到——更新检查硬编码请求 `https://www.aykeji.cn/api/app-update/pandasurvey`
（`web/js/about.js`），那是作者的服务；自建分发请自行改该 URL 或忽略自动更新提示。

**Q：生成的 .fpk 会提交进 git 吗？**
不会。`dist/`、`*.fpk`、`fpk/app/server/panda-survey` 均在 `.gitignore` 中，
打包二进制与产物只存在于本地。
