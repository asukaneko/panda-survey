#!/usr/bin/env bash
# 每日备份：保留最近 30 份。cron 示例（每天凌晨 3 点）：
#   0 3 * * * /opt/panda-survey/scripts/backup.sh /opt/panda-survey
set -euo pipefail

BASE_DIR="${1:-.}"
DB_PATH="${DB_PATH:-$BASE_DIR/panda.db}"
BACKUP_DIR="$BASE_DIR/backups"
KEEP=30

mkdir -p "$BACKUP_DIR"
STAMP="$(date +%F)"
OUT="$BACKUP_DIR/panda_$STAMP.db"

if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 "$DB_PATH" ".backup '$OUT'"
else
  # 无 sqlite3 CLI 时退化为文件复制（建议停服或接受 WAL 期间副本）
  cp "$DB_PATH" "$OUT"
fi

# 保留最近 KEEP 份
ls -1t "$BACKUP_DIR"/panda_*.db 2>/dev/null | tail -n +$((KEEP + 1)) | xargs -r rm -f
echo "备份完成: $OUT"
