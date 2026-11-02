#!/usr/bin/env bash
# devtest：不装 Minecraft，验证桥接模组。
#   1. 构建主 jar 和备用 jar（调用 ../build.sh，里面会检查 class 版本、jdeps、两份 toml）
#   2. 用加载器同款的 TOML 库（Forge 1.20.1：night-config 3.6.4；NeoForge 1.21.1：3.8.3）检查两份 toml
#   3. 用假 SVC 按 SVC 的方式找到插件、跑完整流程（握手、推送、单连接、断线重连、心跳、自测、热重载、停服重启）
#
# 需要 JDK 17+ 和 Go。SVC API 的取法见 ../build.sh；night-config 等几个库会从 Maven Central 下载到缓存目录。
# 环境变量：
#   SVC_API_JAR       SVC API 2.6.x 的 jar（默认 ../.cache/voicechat-api-2.6.27.jar）
#   SVC_OLD_API_JARS  可选，空格分隔的旧版 API jar（比如 2.5.x），再用它们各跑一遍，验证旧版兼容模式
#   MINEDECK_CACHE    缓存目录（默认 ../.cache）
#   DEVTEST_OUT       输出目录（默认 ../build/devtest）
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
repo=$(cd "$root/../.." && pwd)
cache=${MINEDECK_CACHE:-$root/.cache}
api=${SVC_API_JAR:-$cache/voicechat-api-2.6.27.jar}
out=${DEVTEST_OUT:-$root/build/devtest}
mkdir -p "$cache" "$out"

fail() { echo "错误：$*" >&2; exit 1; }
# 不管系统区域设置是什么，Java 的输出都用 UTF-8（JDK 17 认 sun.*，JDK 19 起认 stdout.encoding）
java_opts=(-Dstdout.encoding=UTF-8 -Dstderr.encoding=UTF-8 -Dsun.stdout.encoding=UTF-8 -Dsun.stderr.encoding=UTF-8)
[ -f "$api" ] || fail "找不到 SVC API：$api（见 ../build.sh 的说明）"

echo "### 1. 构建"
"$root/build.sh" --api "$api" --fallback --out "$out/jar"
(cd "$repo" && go build -o "$out/testsend" ./bridge/voicechat/testsend)

# 从 Maven Central 取依赖并校验 sha1
fetch() { # $1=group/path $2=文件名 $3=sha1
  local f=$cache/$2
  if [ ! -f "$f" ]; then
    curl -fsSL --retry 5 --retry-delay 10 -o "$f.part" "https://repo.maven.apache.org/maven2/$1/$2"
    mv "$f.part" "$f"
  fi
  [ "$(sha1sum "$f" | cut -c1-40)" = "$3" ] || fail "$2 校验失败"
  echo "$f"
}
nc_core_old=$(fetch com/electronwill/night-config/core/3.6.4 core-3.6.4.jar 510f174abbf1c947494db50ef2445683bd52c230)
nc_toml_old=$(fetch com/electronwill/night-config/toml/3.6.4 toml-3.6.4.jar 51d6cefb2b55ee55ee26b16391212fb2c7dfb4f4)
nc_core_new=$(fetch com/electronwill/night-config/core/3.8.3 core-3.8.3.jar b442a95f09e349927f5a945ecb594455870fcf4f)
nc_toml_new=$(fetch com/electronwill/night-config/toml/3.8.3 toml-3.8.3.jar 90b2fd6efcb4a7d5b9810cf78f9d824994d717f2)
maven_artifact=$(fetch org/apache/maven/maven-artifact/3.8.5 maven-artifact-3.8.5.jar 4433f50c07debefaed0553bd0068f4f48d449313)
lang3=$(fetch org/apache/commons/commons-lang3/3.12.0 commons-lang3-3.12.0.jar c6842c86792ff03b9f1d1fe2aab8dc23aa6c6f0e)

echo "### 2. 编译 devtest"
rm -rf "$out/classes" "$out/toml-classes"
mapfile -t sources < <(find "$here/src" -name '*.java' ! -name TomlCheck.java | LC_ALL=C sort)
javac -encoding UTF-8 -nowarn -cp "$api" -d "$out/classes" "${sources[@]}"
javac -encoding UTF-8 -nowarn -cp "$nc_core_new:$nc_toml_new:$maven_artifact" -d "$out/toml-classes" "$here/src/devtest/TomlCheck.java"

echo "### 3. TOML 检查（加载器同款的解析库）"
for jar in "$out/jar/minedeck-voicebridge.jar" "$out/jar/minedeck-voicebridge-javafml.jar"; do
  echo "--- $(basename "$jar")"
  java "${java_opts[@]}" -cp "$out/toml-classes:$nc_core_old:$nc_toml_old:$maven_artifact:$lang3" devtest.TomlCheck "$jar" forge 47.4.10 3.6.4
  java "${java_opts[@]}" -cp "$out/toml-classes:$nc_core_new:$nc_toml_new:$maven_artifact:$lang3" devtest.TomlCheck "$jar" neoforge 4.0.45 3.8.3
done

run_fake() { # $1=模组 jar $2=SVC API jar $3=说明
  echo
  echo "### 4. 假 SVC 跑完整流程：$(basename "$1")，SVC API $3"
  local work=$out/work-$(basename "$1" .jar)-$3
  rm -rf "$work"
  java "${java_opts[@]}" -Dminedeck.voicebridge.stdoutLog=true -cp "$out/classes:$2:$1" devtest.FakeSvc \
    --jar "$1" --testsend "$out/testsend" --work "$work"
}

run_fake "$out/jar/minedeck-voicebridge.jar" "$api" "$(basename "$api" .jar | sed 's/^voicechat-api-//')"
run_fake "$out/jar/minedeck-voicebridge-javafml.jar" "$api" "$(basename "$api" .jar | sed 's/^voicechat-api-//')"
for old in ${SVC_OLD_API_JARS:-}; do
  run_fake "$out/jar/minedeck-voicebridge.jar" "$old" "$(basename "$old" .jar | sed 's/^voicechat-api-//')"
done
echo
echo "devtest 全部通过"
