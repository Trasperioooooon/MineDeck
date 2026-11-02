#!/usr/bin/env bash
# 更新打进 MineDeck exe 的伙伴模组 jar（internal/companion/jar/，经 go:embed 打包），或者检查入库的是不是最新的。
#
#   ./update-embedded.sh            重新构建两个加载器的 jar，复制进 internal/companion/jar/，
#                                   并把 manifest.json 里的模组版本、文件名、大小、sha256 改成新的
#   ./update-embedded.sh --check    重新构建，和入库的 jar、manifest.json 里登记的逐一比较；
#                                   都一样退出码 0，不一样退出码 1（改了模组源码却忘了更新时会这样）
#
# 构建用两个工程自己的 gradlew（--no-daemon，用完不留守护进程；内存上限见各自的 gradle.properties）。
# 要装 JDK 21（NeoForge 版）和 JDK 17（Forge 版），Gradle 按工程里写的 toolchain 自己找。
# 构建是可复现的（固定时间戳、固定文件顺序），但不同大版本的 javac 编出的 class 可能不一样：
# 入库的 jar 是用 OpenJDK 21.0.12 和 17.0.20 构建的。第一次构建要下载 Minecraft、加载器和依赖，
# 想把下载的东西放到别处，设 GRADLE_USER_HOME。
# manifest.json 里依赖（PlayerEngine、Architectury API）的版本范围、Modrinth 信息是手工维护的，
# 这个脚本不改它们；它们和 jar 里 mods.toml 对不对得上，由 go test ./internal/companion/jar 检查。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/.." && pwd)
dest=$repo/internal/companion/jar
manifest=$dest/manifest.json
check=0
while [ $# -gt 0 ]; do
  case "$1" in
    --check) check=1; shift ;;
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
    *) echo "不认识的参数：$1" >&2; exit 2 ;;
  esac
done

fail() { echo "错误：$*" >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

prop() { sed -n "s/^$2=//p" "$1/gradle.properties" | tr -d '\r'; }

# 工程目录、jar 的基本名（和 build.gradle 的 archivesName、清单里的 file 一致）
projects=(neoforge-1.21.1 forge-1.20.1)
version=""
for p in "${projects[@]}"; do
  v=$(prop "$here/$p" mod_version)
  [ -n "$v" ] || fail "读不到 $p/gradle.properties 里的 mod_version"
  if [ -z "$version" ]; then version=$v; elif [ "$v" != "$version" ]; then
    fail "两个工程的 mod_version 不一样（$version、$v），要一起改"
  fi
done

for p in "${projects[@]}"; do
  echo "构建 $p（版本 $version）……"
  (cd "$here/$p" && ./gradlew --no-daemon -q clean build) > "$tmp/build-$p.log" 2>&1 || { cat "$tmp/build-$p.log" >&2; fail "$p 构建失败"; }
  out=$here/$p/build/libs/minedeck-companion-$p-$version.jar
  [ -f "$out" ] || fail "没找到构建结果 $out"
  cp "$out" "$tmp/minedeck-companion-$p.jar"
done

echo "构建结果（版本 $version）："
for p in "${projects[@]}"; do
  f=$tmp/minedeck-companion-$p.jar
  echo "  $(sha256sum "$f" | cut -c1-64)  $(stat -c %s "$f") 字节  minedeck-companion-$p.jar"
done

# 清单里某个 jar 那一段的某个字段（按 "file" 那一行定位到这一段）
field() { # $1=jar 文件名 $2=字段名
  awk -v file="$1" -v key="$2" '
    /"file": "/ { cur = ($0 ~ "\"file\": \"" file "\"") }
    cur && $0 ~ "\"" key "\": " { sub(/^[^:]*: */, ""); sub(/,$/, ""); gsub(/"/, ""); print; exit }
  ' "$manifest"
}

if [ "$check" = 1 ]; then
  bad=0
  grep -q "^    \"version\": \"$version\",$" "$manifest" || { echo "  manifest.json 的 mod.version 不是 $version"; bad=1; }
  for p in "${projects[@]}"; do
    j=minedeck-companion-$p.jar
    if cmp -s "$tmp/$j" "$dest/$j"; then
      echo "  一致：internal/companion/jar/$j"
    else
      echo "  不一致：internal/companion/jar/$j（模组源码改了之后没有运行 ./update-embedded.sh？）"
      bad=1
    fi
    [ "$(field "$j" sha256)" = "$(sha256sum "$tmp/$j" | cut -c1-64)" ] || { echo "  manifest.json 登记的 $j 的 sha256 不对"; bad=1; }
    [ "$(field "$j" size)" = "$(stat -c %s "$tmp/$j")" ] || { echo "  manifest.json 登记的 $j 的 size 不对"; bad=1; }
    [ "$(field "$j" installName)" = "minedeck-companion-$p-$version.jar" ] || { echo "  manifest.json 登记的 $j 的 installName 不对"; bad=1; }
  done
  [ "$bad" = 0 ] || exit 1
  echo "入库的 jar 和 manifest.json 是最新的。"
  exit 0
fi

mkdir -p "$dest"
for p in "${projects[@]}"; do
  cp "$tmp/minedeck-companion-$p.jar" "$dest/"
done

# 改清单：mod.version，以及每个 jar 那一段的 installName、size、sha256（别的字段不动）
args=()
for p in "${projects[@]}"; do
  f=$dest/minedeck-companion-$p.jar
  args+=("minedeck-companion-$p.jar=minedeck-companion-$p-$version.jar=$(stat -c %s "$f")=$(sha256sum "$f" | cut -c1-64)")
done
awk -v version="$version" -v specs="${args[*]}" '
  BEGIN {
    n = split(specs, list, " ")
    for (i = 1; i <= n; i++) { split(list[i], p, "="); inst[p[1]] = p[2]; size[p[1]] = p[3]; sum[p[1]] = p[4] }
  }
  /^    "version": "/ && !done_version { sub(/"version": "[^"]*"/, "\"version\": \"" version "\""); done_version = 1 }
  /"file": "/ { cur = ""; for (f in inst) if ($0 ~ "\"file\": \"" f "\"") cur = f }
  cur != "" && /"installName": "/ { sub(/"installName": "[^"]*"/, "\"installName\": \"" inst[cur] "\"") }
  cur != "" && /"size": [0-9]+,/ && !(cur in size_done) { sub(/"size": [0-9]+/, "\"size\": " size[cur]); size_done[cur] = 1 }
  cur != "" && /"sha256": "/ { sub(/"sha256": "[0-9a-f]*"/, "\"sha256\": \"" sum[cur] "\""); cur = "" }
  { print }
' "$manifest" > "$tmp/manifest.json"
mv "$tmp/manifest.json" "$manifest"

for p in "${projects[@]}"; do
  j=minedeck-companion-$p.jar
  [ "$(field "$j" sha256)" = "$(sha256sum "$dest/$j" | cut -c1-64)" ] && [ "$(field "$j" size)" = "$(stat -c %s "$dest/$j")" ] \
    && [ "$(field "$j" installName)" = "minedeck-companion-$p-$version.jar" ] || fail "没能改好 $manifest 里 $j 的那一段，请手动改"
done
echo "已更新 internal/companion/jar/ 和 manifest.json。记得跑一遍 go test ./internal/companion/jar 再提交。"
