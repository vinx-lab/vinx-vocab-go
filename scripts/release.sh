#!/usr/bin/env bash
# 发版：定版本号 → 改 CHANGELOG → 检查 → 提交 → 打标签 → 构建。不推送。
#
#   make release              修订号 +1（v3.1.0 → v3.1.1）；[Unreleased] 里有「### 新增」时拒绝，确认只升修订号加 Z=1
#   make release Y=1          次版本号 +1，修订号归零（v3.1.4 → v3.2.0）
#   make release X=1          主版本号 +1（v3.2.0 → v4.0.0）
#   make release NEW=3.2.0    指定版本号
#   make release DRY=1        只算版本号、预览 CHANGELOG，不改任何东西
#
# 版本号只来自 git 标签 vX.Y.Z；每发一个包修订号 +1，不复用、不跳号。
# 要求：在 main 分支、工作区干净、CHANGELOG 的 [Unreleased] 不为空。
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# 项目配置
APP_NAME="Vinx Vocab"
CHANGELOG=CHANGELOG.md
RELEASE_BRANCH=main
check() { make vet test && pnpm -C web exec vitest run; }
build() { make cross VERSION="$1"; }
built_version() { ./dist/vinx-vocab version | awk '{print $2}'; }

die() { echo "release: $*" >&2; exit 1; }

dry=${DRY:-}
branch=$(git rev-parse --abbrev-ref HEAD)
[[ $branch == "$RELEASE_BRANCH" ]] || die "当前在 $branch，发版只在 $RELEASE_BRANCH 上做"
if [[ -z $dry ]]; then
  [[ -z $(git status --porcelain) ]] || { git status --short >&2; die "工作区有未提交的改动或未跟踪的文件，先提交或清理"; }
fi

# 上一个版本：所有 vX.Y.Z 标签里最大的
last=$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n1 || true)
[[ -n $last ]] || die "没有 vX.Y.Z 标签，首个版本请用 NEW=x.y.z 指定"
IFS=. read -r x y z <<<"${last#v}"

if [[ -n ${NEW:-} ]]; then
  [[ $NEW =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "NEW 要写成 x.y.z（不带 v）：$NEW"
  new=$NEW
elif [[ -n ${X:-} ]]; then
  new="$((x + 1)).0.0"
elif [[ -n ${Y:-} ]]; then
  new="$x.$((y + 1)).0"
else
  new="$x.$y.$((z + 1))"
fi
tag="v$new"

git rev-parse -q --verify "refs/tags/$tag" >/dev/null && die "标签 $tag 已存在"
newest=$(printf '%s\n%s\n' "$last" "$tag" | sort -V | tail -n1)
[[ $newest == "$tag" && $tag != "$last" ]] || die "$tag 不比上一个版本 $last 新"
if remote_tags=$(timeout 15 git ls-remote --tags origin "refs/tags/$tag" 2>/dev/null); then
  [[ -z $remote_tags ]] || die "远程已有标签 $tag"
else
  echo "release: 连不上 origin，跳过远程标签检查" >&2
fi
[[ -n $(git log --oneline "$last..HEAD") ]] || die "$last 之后没有新提交"

# [Unreleased] 一节的正文（到下一个 ## 为止）
unreleased=$(awk '/^## \[Unreleased\]/ { f = 1; next } f && /^## / { exit } f' "$CHANGELOG")
if [[ -z ${unreleased//[[:space:]]/} ]]; then
  echo "$last 以来的提交：" >&2
  git log --oneline "$last..HEAD" >&2
  die "$CHANGELOG 的 [Unreleased] 是空的，先按「新增 / 修复」写好再发版"
fi

if [[ -z ${NEW:-}${X:-}${Y:-}${Z:-} ]] && grep -q '^### 新增' <<<"$unreleased"; then
  die "[Unreleased] 里有「### 新增」，按语义化版本应升次版本号：make release Y=1（确定只升修订号用 Z=1）"
fi

repo_url=$(git remote get-url origin | sed -E 's#^git@github.com:#https://github.com/#; s#\.git$##')
today=$(date +%F)

# 改 CHANGELOG：[Unreleased] 下插入新版本标题；底部链接改 Unreleased 的对比基准，并加新版本链接
tmp=$(mktemp)
awk -v ver="$new" -v tag="$tag" -v day="$today" -v url="$repo_url" '
  /^## \[Unreleased\]/ { print; print ""; print "## [" ver "] - " day; print ""; skip_blank = 1; next }
  skip_blank && /^[[:space:]]*$/ { next }
  { skip_blank = 0 }
  /^\[Unreleased\]: / { print "[Unreleased]: " url "/compare/" tag "...HEAD"; print "[" ver "]: " url "/releases/tag/" tag; next }
  { print }
' "$CHANGELOG" >"$tmp"
grep -q "^\[$new\]: " "$tmp" || { rm -f "$tmp"; die "$CHANGELOG 底部没有 [Unreleased]: 链接行，无法更新"; }

echo "上一个版本 $last → 新版本 $tag（$today）"
if [[ -n $dry ]]; then
  echo "--- CHANGELOG 改动预览（DRY=1，未写入）---"
  diff -u "$CHANGELOG" "$tmp" || true
  rm -f "$tmp"
  exit 0
fi

trap 'rm -f "$tmp"' EXIT
check
cat "$tmp" >"$CHANGELOG"
rm -f "$tmp"
git add "$CHANGELOG"
git commit -q -m "chore: 发布 $new"
git tag -a "$tag" -m "$APP_NAME $new"
build "$tag"
got=$(built_version)
[[ $got == "$tag" ]] || die "构建出的版本是 $got，不是 $tag"

cat <<EOF

已提交并打标签 $tag（只在本地），构建产物在 dist/。
试运行稳定后再推送公开：
  git push origin $RELEASE_BRANCH $tag
推送标签会触发 Release 工作流，用 CHANGELOG 的 [$new] 一节建 Release。
EOF
