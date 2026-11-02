#!/usr/bin/env bash
# 构建 MineDeck 音乐共享桥接模组：一个 jar 同时给 NeoForge 1.21.1 和 Forge 1.20.1 用。
# 只需要 JDK 17 或更新（用 --release 8 编译出 Java 8 字节码），不需要 Gradle。
#
# 用法：
#   ./build.sh                    构建主 jar（lowcodefml，没有入口类）
#   ./build.sh --fallback         另外构建备用 jar（javafml + 两个空入口类）
#   ./build.sh --api <jar>        指定 SVC API jar（也可以用环境变量 SVC_API_JAR）
#   ./build.sh --out <目录>       输出目录（默认 build/）
#
# SVC API 只在编译时用，绝不打进 jar。查找顺序：--api / SVC_API_JAR → 缓存目录里的
# voicechat-api-<版本>.jar → 从 maven.maxhenkel.de 下载到缓存目录。缓存目录默认是 .cache/，
# 可以用环境变量 MINEDECK_CACHE 改。下载不了时，用 ./api-from-source.sh 从 SVC 官方源码编译一份。
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
api_version=2.6.27
mod_version=0.2.0
cache=${MINEDECK_CACHE:-$here/.cache}
out_dir=$here/build
api_jar=${SVC_API_JAR:-}
fallback=0
# 固定 jar 里的时间戳，同样的源码每次构建出的 jar 逐字节相同
jar_date=${JAR_DATE:-2026-01-01T00:00:00Z}

while [ $# -gt 0 ]; do
  case "$1" in
    --api) api_jar=$2; shift 2 ;;
    --out) out_dir=$2; shift 2 ;;
    --fallback) fallback=1; shift ;;
    -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "不认识的参数：$1" >&2; exit 2 ;;
  esac
done

fail() { echo "错误：$*" >&2; exit 1; }

command -v javac >/dev/null || fail "找不到 javac，请安装 JDK 17 或更新版本"
command -v jar >/dev/null || fail "找不到 jar（JDK 自带）"

# ---------- 1. SVC API ----------
if [ -z "$api_jar" ]; then
  api_jar=$cache/voicechat-api-$api_version.jar
  if [ ! -f "$api_jar" ]; then
    url=https://maven.maxhenkel.de/repository/public/de/maxhenkel/voicechat/voicechat-api/$api_version/voicechat-api-$api_version.jar
    echo "下载 SVC API $api_version（只用于编译）：$url"
    mkdir -p "$cache"
    if curl -fsSL -o "$api_jar.part" "$url" && curl -fsSL -o "$api_jar.sha1" "$url.sha1"; then
      want=$(cut -c1-40 "$api_jar.sha1")
      got=$(sha1sum "$api_jar.part" | cut -c1-40)
      [ "$want" = "$got" ] || { rm -f "$api_jar.part"; fail "SVC API 校验失败（sha1 应为 $want，实际 $got）"; }
      mv "$api_jar.part" "$api_jar"
    else
      rm -f "$api_jar.part" "$api_jar.sha1"
      fail "下载 SVC API 失败。可以：
  1) 手动下载 voicechat-api-$api_version.jar，然后 ./build.sh --api <路径>；或
  2) 运行 ./api-from-source.sh，从 SVC 官方源码编译一份（只用于编译，不入库）。"
    fi
  fi
fi
[ -f "$api_jar" ] || fail "SVC API jar 不存在：$api_jar"
echo "SVC API（仅编译期）：$api_jar"

# ---------- 2. 编译 ----------
build=$out_dir/work
rm -rf "$build"
mkdir -p "$build/main" "$build/stubs" "$build/fallback" "$out_dir"

javac_flags=(--release 8 -encoding UTF-8 -Xlint:all,-options -g)
mapfile -t main_sources < <(find "$here/src/main/java" -name '*.java' | LC_ALL=C sort)
javac "${javac_flags[@]}" -cp "$api_jar" -d "$build/main" "${main_sources[@]}"
cp -R "$here/src/main/resources/." "$build/main/"

# 自己写 Created-By：不写的话 jar 会填上 JDK 的版本号，换一个 JDK 小版本构建出的 jar 就不一样了
write_manifest() {
  printf 'Manifest-Version: 1.0\nCreated-By: MineDeck bridge/voicechat build script\nImplementation-Title: MineDeck Voice Bridge\nImplementation-Version: %s\nImplementation-Vendor: MineDeck\n' "$mod_version" > "$1"
}

# 按排好序的文件列表打包，并固定时间戳，保证可复现
make_jar() { # $1=类目录 $2=输出 jar
  local dir=$1 jarfile=$2 mf
  mf=$(mktemp)
  write_manifest "$mf"
  rm -f "$jarfile"
  (cd "$dir" && find . -type f | LC_ALL=C sort | sed 's|^\./||' > "$build/files.txt" \
    && jar --create --date="$jar_date" --file "$jarfile" --manifest "$mf" @"$build/files.txt")
  rm -f "$mf"
}

main_jar=$out_dir/minedeck-voicebridge.jar
make_jar "$build/main" "$main_jar"
echo "已生成：$main_jar"

if [ "$fallback" = 1 ]; then
  mapfile -t stub_sources < <(find "$here/src/fallback/stubs" -name '*.java' | LC_ALL=C sort)
  javac "${javac_flags[@]}" -d "$build/stubs" "${stub_sources[@]}"
  cp -R "$build/main/." "$build/fallback/"
  mapfile -t fb_sources < <(find "$here/src/fallback/java" -name '*.java' | LC_ALL=C sort)
  javac "${javac_flags[@]}" -cp "$build/stubs" -d "$build/fallback" "${fb_sources[@]}"
  # 同样的两份 toml，只把加载方式换成 javafml（版本范围两边都照用：Forge 47 / NeoForge 1）
  for f in mods.toml neoforge.mods.toml; do
    sed -e 's/^modLoader="lowcodefml"$/modLoader="javafml"/' \
        "$here/src/main/resources/META-INF/$f" > "$build/fallback/META-INF/$f"
    grep -q '^modLoader="javafml"$' "$build/fallback/META-INF/$f" || fail "备用 jar 的 $f 没改成 javafml"
  done
  fallback_jar=$out_dir/minedeck-voicebridge-javafml.jar
  make_jar "$build/fallback" "$fallback_jar"
  echo "已生成：$fallback_jar"
fi

# ---------- 3. 检查 ----------
check_jar() { # $1=jar $2=允许依赖的额外包（正则） $3=额外包的说明
  local jarfile=$1 extra=$2 extra_label=${3:-} list bad deps
  echo "== 检查 $(basename "$jarfile")"
  list=$(jar tf "$jarfile")
  for need in META-INF/mods.toml META-INF/neoforge.mods.toml pack.mcmeta minedeck/voicebridge/BridgePlugin.class; do
    grep -qx "$need" <<<"$list" || fail "jar 里缺少 $need"
  done
  if grep -q '^de/maxhenkel/' <<<"$list"; then fail "jar 里不应该有 SVC 的文件"; fi
  echo "  两份 toml 都在；没有 SVC 的文件"

  # 每个 class 的主版本号都应该是 52（Java 8）
  local tmp; tmp=$(mktemp -d)
  (cd "$tmp" && jar xf "$jarfile")
  bad=0
  while IFS= read -r c; do
    major=$(od -An -j6 -N2 -tu1 "$c" | awk '{print $1*256+$2}')
    if [ "$major" != 52 ]; then echo "  $c 的版本号是 $major，不是 52"; bad=1; fi
  done < <(find "$tmp" -name '*.class')
  rm -rf "$tmp"
  [ "$bad" = 0 ] || fail "有 class 不是 Java 8 字节码"
  echo "  所有 class 的版本号都是 52（Java 8）"

  # 依赖只能是 java.*、SVC API 和自己
  if command -v jdeps >/dev/null; then
    deps=$(jdeps -verbose:package -cp "$api_jar" "$jarfile" 2>&1 | grep -v '^Picked up' \
      | awk '/^ +/ && $2=="->" {print $3}' | sort -u) || fail "jdeps 运行失败"
    bad=$(grep -Ev "^(java\.|de\.maxhenkel\.voicechat\.api(\.|$)|minedeck\.voicebridge(\.|$)${extra:+|$extra})" <<<"$deps" || true)
    [ -z "$bad" ] || fail "发现不允许的依赖：$bad"
    echo "  jdeps：依赖只有 java.*、de.maxhenkel.voicechat.api.*${extra_label:+ 和 $extra_label}，没有 Minecraft 的类"
  else
    echo "  （没有 jdeps，跳过依赖检查）"
  fi

  javap -v -cp "$jarfile" minedeck.voicebridge.BridgePlugin | grep -q 'Lde/maxhenkel/voicechat/api/ForgeVoicechatPlugin;' \
    || fail "BridgePlugin 上没有 @ForgeVoicechatPlugin 注解"
  echo "  BridgePlugin 带 @ForgeVoicechatPlugin（运行时可见注解）"
}

check_jar "$main_jar" ""
if [ "$fallback" = 1 ]; then
  check_jar "$fallback_jar" 'net\.minecraftforge\.fml\.common$|net\.neoforged\.fml\.common$' \
    "两个加载器的 @Mod 注解（只在空入口类上）"
fi
echo "完成。sha256："
if [ "$fallback" = 1 ]; then
  sha256sum "$main_jar" "$fallback_jar"
else
  sha256sum "$main_jar"
fi
