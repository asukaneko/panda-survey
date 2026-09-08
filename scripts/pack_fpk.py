#!/usr/bin/env python3
"""fnOS FPK 打包脚本：把编译好的 panda-survey 二进制 + fpk 骨架打成 .fpk 安装包。

FPK 结构（与作者发布的 pandasurvey-0.2.0-x86.fpk 逐字节核对）：
    gzip( ustar tar，包含：
        app.tgz             # app 负载的 tar.gz（server/panda-survey + ui/ + config/）
        LICENSE
        cmd/                # 生命周期脚本（main 等 9 个）
        config/             # privilege + resource
        ICON.PNG ICON_256.PNG
        manifest            # 追加一行 checksum = md5(app.tgz)
        wizard/uninstall
    )

用法：
    python scripts/pack_fpk.py --skeleton fpk --binary dist/panda-survey-linux-amd64 \
        --version 0.3.0 --platform x86 --out dist
"""
import argparse
import gzip
import hashlib
import io
import os
import sys
import tarfile

# 与参考包一致的元数据：uid/gid 0、目录 0777、文件 0666、统一 mtime（2026-08-24）
MODE_DIR = 0o777
MODE_FILE = 0o666
MTIME = 1787574163

# 内层 tar 的条目顺序（与参考包一致）
INNER_ORDER = [
    "app.tgz",
    "LICENSE",
    "cmd", "cmd/config_callback", "cmd/config_init", "cmd/install_callback",
    "cmd/install_init", "cmd/main", "cmd/uninstall_callback", "cmd/uninstall_init",
    "cmd/upgrade_callback", "cmd/upgrade_init",
    "config", "config/privilege", "config/resource",
    "ICON.PNG", "ICON_256.PNG",
    "manifest",
    "wizard", "wizard/uninstall",
]

# app.tgz 内的条目顺序（与参考包一致）
APP_ORDER = [
    "server", "server/panda-survey",
    "ui", "ui/config", "ui/images", "ui/images/icon_256.png", "ui/images/icon_64.png",
    "config", "config/privilege", "config/resource",
]

MANIFEST_CHECKSUM_PAD = 22  # "checksum" 左对齐到 22 列，与参考包对齐方式一致


def make_tarinfo(name: str, is_dir: bool, size: int = 0) -> tarfile.TarInfo:
    ti = tarfile.TarInfo(name)
    ti.mode = MODE_DIR if is_dir else MODE_FILE
    ti.uid = 0
    ti.gid = 0
    ti.uname = ""
    ti.gname = ""
    ti.mtime = MTIME
    ti.size = size
    ti.type = tarfile.DIRTYPE if is_dir else tarfile.REGTYPE
    return ti


def build_tar(entries, order) -> bytes:
    """entries: {name: 文件字节 或 None(目录)}，按 order 顺序写入 ustar tar"""
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w", format=tarfile.USTAR_FORMAT) as tf:
        for name in order:
            data = entries[name]
            if data is None:
                tf.addfile(make_tarinfo(name, True))
            else:
                ti = make_tarinfo(name, False, len(data))
                tf.addfile(ti, io.BytesIO(data))
    return buf.getvalue()


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--skeleton", required=True, help="fpk 骨架目录（含 manifest/cmd/config/app/...）")
    ap.add_argument("--binary", required=True, help="编译好的 linux 二进制路径")
    ap.add_argument("--version", required=True, help="版本号，如 0.3.0")
    ap.add_argument("--platform", required=True, choices=["x86", "arm"], help="x86(amd64) 或 arm")
    ap.add_argument("--out", required=True, help="输出目录")
    args = ap.parse_args()

    sk = os.path.abspath(args.skeleton)
    binary = os.path.abspath(args.binary)
    if not os.path.isfile(binary):
        sys.exit(f"二进制不存在: {binary}")
    os.makedirs(args.out, exist_ok=True)

    def read(p):
        with open(os.path.join(sk, p), "rb") as f:
            return f.read()

    # 1) app.tgz：app 负载（server 二进制 + ui + config）
    app_data = {name: None for name in APP_ORDER if name in ("server", "ui", "ui/images", "config")}
    app_data["server/panda-survey"] = open(binary, "rb").read()
    # ui 取自骨架 app/ui/；config 取自骨架根部 config/（与参考包 app.tgz 内容一致）
    app_data["ui/config"] = read(os.path.join("app", "ui", "config"))
    app_data["ui/images/icon_256.png"] = read(os.path.join("app", "ui", "images", "icon_256.png"))
    app_data["ui/images/icon_64.png"] = read(os.path.join("app", "ui", "images", "icon_64.png"))
    app_data["config/privilege"] = read("config/privilege")
    app_data["config/resource"] = read("config/resource")
    app_tgz = gzip.compress(build_tar(app_data, APP_ORDER), mtime=0)

    # 2) manifest：骨架 manifest + checksum = md5(app.tgz)
    manifest = read("manifest")
    md5 = hashlib.md5(app_tgz).hexdigest()
    checksum_line = f"{'checksum':<{MANIFEST_CHECKSUM_PAD}}= {md5}\n".encode("utf-8")
    manifest_out = manifest + b"\n" + checksum_line

    # 3) 内层 tar
    inner_data = {"app.tgz": app_tgz, "manifest": manifest_out}
    for name in INNER_ORDER:
        if name in inner_data:
            continue
        if name in ("cmd", "config", "wizard"):
            inner_data[name] = None  # 目录
        else:
            inner_data[name] = read(name)
    inner = build_tar(inner_data, INNER_ORDER)

    # 4) gzip -> fpk
    fpk = gzip.compress(inner, mtime=0)
    out_name = f"pandasurvey-{args.version}-{args.platform}.fpk"
    out_path = os.path.join(args.out, out_name)
    with open(out_path, "wb") as f:
        f.write(fpk)
    print(f"app.tgz md5     : {md5}")
    print(f"app.tgz size    : {len(app_tgz)}")
    print(f"inner tar size  : {len(inner)}")
    print(f"fpk size        : {len(fpk)}")
    print(f"output          : {out_path}")
    return out_path


if __name__ == "__main__":
    main()
