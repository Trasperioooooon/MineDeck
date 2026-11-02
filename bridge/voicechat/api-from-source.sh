#!/usr/bin/env bash
# 从 SVC 官方源码（github.com/henkelmax/simple-voice-chat 的 api/ 目录）编译 voicechat-api。
# 只在 maven.maxhenkel.de 下载不了时用。产物只用于本地编译和 devtest：
# 不提交进仓库、不打进模组 jar、不分发（SVC 的许可是"保留所有权利"）。
#
# 用法：
#   ./api-from-source.sh [--src <已有的 simple-voice-chat 检出>] [--commit <提交>] [--out <输出 jar>]
#                        [--jsr305 <jsr305-3.0.2.jar>]
# 默认提交是 1.21.1 分支 2026-10-25 的版本（API 2.6.27，和 1.20.1 分支的 api/ 目录逐字节相同）。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
cache=${MINEDECK_CACHE:-$here/.cache}
commit=42833f14101277cc36e4a52b7a16ea1b50375442
src=""
out=""
jsr305=""

while [ $# -gt 0 ]; do
  case "$1" in
    --src) src=$2; shift 2 ;;
    --commit) commit=$2; shift 2 ;;
    --out) out=$2; shift 2 ;;
    --jsr305) jsr305=$2; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "不认识的参数：$1" >&2; exit 2 ;;
  esac
done

fail() { echo "错误：$*" >&2; exit 1; }
mkdir -p "$cache"

if [ -z "$src" ]; then
  src=$cache/simple-voice-chat
  if [ ! -d "$src/.git" ]; then
    git init -q "$src"
    git -C "$src" remote add origin https://github.com/henkelmax/simple-voice-chat
  fi
  git -C "$src" cat-file -e "$commit^{commit}" 2>/dev/null || git -C "$src" fetch -q --depth 1 origin "$commit"
fi
git -C "$src" cat-file -e "$commit^{commit}" 2>/dev/null || fail "$src 里没有提交 $commit"

version=$(git -C "$src" show "$commit:api/gradle.properties" | sed -n 's/^version=//p' | tr -d '\r')
[ -n "$version" ] || fail "读不到 API 版本号"
[ -n "$out" ] || out=$cache/voicechat-api-$version.jar

# API 源码用了 javax.annotation.Nullable（jsr305），从 Maven Central 取，校验 sha1
if [ -z "$jsr305" ]; then
  jsr305=$cache/jsr305-3.0.2.jar
  if [ ! -f "$jsr305" ]; then
    url=https://repo.maven.apache.org/maven2/com/google/code/findbugs/jsr305/3.0.2/jsr305-3.0.2.jar
    curl -fsSL -o "$jsr305.part" "$url" || fail "下载 jsr305 失败：$url"
    got=$(sha1sum "$jsr305.part" | cut -c1-40)
    [ "$got" = 25ea2e8b0c338a877313bd4672d3fe056ea78f0d ] || { rm -f "$jsr305.part"; fail "jsr305 校验失败"; }
    mv "$jsr305.part" "$jsr305"
  fi
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
git -C "$src" archive "$commit" api/src/main/java | tar -x -C "$tmp"
mapfile -t sources < <(find "$tmp/api/src/main/java" -name '*.java' | LC_ALL=C sort)
mkdir -p "$tmp/classes"
javac --release 8 -nowarn -Xlint:-options -encoding UTF-8 -cp "$jsr305" -d "$tmp/classes" "${sources[@]}"
mkdir -p "$(dirname "$out")"
rm -f "$out"
jar --create --file "$out" -C "$tmp/classes" .
echo "已编译 SVC API $version（提交 ${commit:0:10}）：$out"
