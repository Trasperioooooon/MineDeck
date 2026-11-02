#!/usr/bin/env bash
# 更新打进 MineDeck exe 的桥接模组 jar（internal/bridge/jar/，经 go:embed 打包），或者检查入库的是不是最新的。
#
#   ./update-embedded.sh [--api <jar>]          重新构建主 jar 和备用 jar，复制进 internal/bridge/jar/，
#                                                并把 internal/bridge/jar.go 里的版本号和 sha256 改成新的
#   ./update-embedded.sh --check [--api <jar>]   重新构建，和入库的 jar、jar.go 里登记的 sha256 逐一比较；
#                                                都一样退出码 0，不一样退出码 1（改了模组源码却忘了更新时会这样）
#
# 构建用 build.sh（SVC API 的取法见那里：--api / SVC_API_JAR / 缓存目录 / 下载）。要用 JDK 21：
# 构建是可复现的，但不同大版本的 javac 编出的 class 可能不一样。入库的 jar 是用 OpenJDK 21.0.12 构建的。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
dest=$repo/internal/bridge/jar
gofile=$repo/internal/bridge/jar.go
check=0
api_args=()
while [ $# -gt 0 ]; do
  case "$1" in
    --check) check=1; shift ;;
    --api) api_args=(--api "$2"); shift 2 ;;
    -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
    *) echo "不认识的参数：$1" >&2; exit 2 ;;
  esac
done

fail() { echo "错误：$*" >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

"$here/build.sh" "${api_args[@]}" --fallback --out "$tmp" > "$tmp/build.log" 2>&1 || { cat "$tmp/build.log" >&2; fail "构建失败"; }
version=$(sed -n 's/^mod_version=//p' "$here/build.sh")
[ -n "$version" ] || fail "读不到 build.sh 里的 mod_version"
main_sum=$(sha256sum "$tmp/minedeck-voicebridge.jar" | cut -c1-64)
fb_sum=$(sha256sum "$tmp/minedeck-voicebridge-javafml.jar" | cut -c1-64)
echo "构建结果（版本 $version）："
echo "  $main_sum  minedeck-voicebridge.jar"
echo "  $fb_sum  minedeck-voicebridge-javafml.jar"

if [ "$check" = 1 ]; then
  bad=0
  for j in minedeck-voicebridge.jar minedeck-voicebridge-javafml.jar; do
    if cmp -s "$tmp/$j" "$dest/$j"; then
      echo "  一致：internal/bridge/jar/$j"
    else
      echo "  不一致：internal/bridge/jar/$j（模组源码改了之后没有运行 ./update-embedded.sh？）"
      bad=1
    fi
  done
  grep -q "JarVersion = \"$version\"" "$gofile" || { echo "  jar.go 的 JarVersion 不是 $version"; bad=1; }
  grep -q "jarMainSHA256     = \"$main_sum\"" "$gofile" || { echo "  jar.go 登记的主 jar sha256 不对"; bad=1; }
  grep -q "jarFallbackSHA256 = \"$fb_sum\"" "$gofile" || { echo "  jar.go 登记的备用 jar sha256 不对"; bad=1; }
  [ "$bad" = 0 ] || exit 1
  echo "入库的 jar 是最新的。"
  exit 0
fi

mkdir -p "$dest"
cp "$tmp/minedeck-voicebridge.jar" "$tmp/minedeck-voicebridge-javafml.jar" "$dest/"
sed -i -e "s/^\(\tJarVersion = \)\"[^\"]*\"/\1\"$version\"/" \
       -e "s/^\(\tjarMainSHA256     = \)\"[0-9a-f]*\"/\1\"$main_sum\"/" \
       -e "s/^\(\tjarFallbackSHA256 = \)\"[0-9a-f]*\"/\1\"$fb_sum\"/" "$gofile"
grep -q "jarMainSHA256     = \"$main_sum\"" "$gofile" && grep -q "jarFallbackSHA256 = \"$fb_sum\"" "$gofile" \
  && grep -q "JarVersion = \"$version\"" "$gofile" || fail "没能改好 $gofile，请手动改"
echo "已更新 internal/bridge/jar/ 和 internal/bridge/jar.go。记得跑一遍 go test ./internal/bridge 再提交。"
