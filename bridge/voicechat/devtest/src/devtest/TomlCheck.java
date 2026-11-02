package devtest;

import com.electronwill.nightconfig.core.CommentedConfig;
import com.electronwill.nightconfig.core.Config;
import com.electronwill.nightconfig.toml.TomlFormat;
import org.apache.maven.artifact.versioning.DefaultArtifactVersion;
import org.apache.maven.artifact.versioning.VersionRange;

import java.io.InputStream;
import java.io.StringReader;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.List;
import java.util.Locale;
import java.util.regex.Pattern;
import java.util.zip.ZipEntry;
import java.util.zip.ZipFile;

/**
 * 不启动游戏，检查 jar 里的 toml 能不能过加载器的解析和校验。
 *
 * <p>用的是加载器同款的库：Forge 1.20.1 带 night-config 3.6.4，NeoForge 1.21.1 带 3.8.3，两边的版本范围都用
 * maven-artifact 3.8.5 解析。校验规则照抄两边加载器源码（文件和行号见 README 的「验证记录」）：
 * <ul>
 *   <li>Forge 1.20.1：fmlloader 的 ModFileInfo / ModInfo / ModSorter，forgespi 7.0.1 的 IModInfo.UNBOUNDED</li>
 *   <li>NeoForge 1.21.1：FancyModLoader 4.0.x 的 ModFileInfo / ModInfo / ModSorter，IModInfo.UNBOUNDED</li>
 * </ul>
 * 用法：TomlCheck &lt;jar&gt; forge|neoforge &lt;语言加载器版本&gt; &lt;night-config 版本（只用于显示）&gt;
 */
public final class TomlCheck {

    private static final Pattern VALID_MODID = Pattern.compile("^[a-z][a-z0-9_]{1,63}$");
    private static final Pattern VALID_VERSION = Pattern.compile("^\\d+.*");
    private static final List<String> ORDERING = Arrays.asList("NONE", "BEFORE", "AFTER");
    private static final List<String> SIDE = Arrays.asList("BOTH", "CLIENT", "SERVER");
    private static final List<String> NEO_TYPES = Arrays.asList("REQUIRED", "OPTIONAL", "INCOMPATIBLE", "DISCOURAGED");
    private static final List<String> DISPLAY_TEST = Arrays.asList("MATCH_VERSION", "IGNORE_SERVER_VERSION", "IGNORE_ALL_VERSION", "NONE");

    private static int failures;

    public static void main(String[] args) throws Exception {
        String jar = args[0];
        boolean forge = args[1].equals("forge");
        String languageVersion = args[2];
        String nightConfig = args.length > 3 ? args[3] : "?";
        String entry = forge ? "META-INF/mods.toml" : "META-INF/neoforge.mods.toml";
        String text;
        try (ZipFile z = new ZipFile(jar)) {
            ZipEntry e = z.getEntry(entry);
            if (e == null) {
                fail("jar 里没有 " + entry);
                System.exit(1);
                return;
            }
            try (InputStream in = z.getInputStream(e)) {
                text = new String(in.readAllBytes(), StandardCharsets.UTF_8);
            }
        }
        String loader = forge ? "Forge 1.20.1" : "NeoForge 1.21.1";
        System.out.println("== " + loader + "：" + entry + "（night-config " + nightConfig + "）");

        CommentedConfig config = TomlFormat.instance().createParser().parse(new StringReader(text));
        if (!forge) {
            // NeoForge 会先写出再读回一遍，做一份不可变的拷贝（ModFileParser.copyConfig）
            String round = TomlFormat.instance().createWriter().writeToString(config);
            config = TomlFormat.instance().createParser().parse(new StringReader(round));
        }
        ok("TOML 解析通过");

        String modLoader = config.get("modLoader");
        check(modLoader != null, "modLoader = " + modLoader);
        String loaderVersion = config.get("loaderVersion");
        check(loaderVersion != null, "loaderVersion = " + loaderVersion);
        VersionRange range = VersionRange.createFromVersionSpec(loaderVersion);
        check(range.containsVersion(new DefaultArtifactVersion(languageVersion)),
                "loaderVersion " + loaderVersion + " 包含 " + modLoader + " 的版本 " + languageVersion);
        String license = config.get("license");
        check(license != null && !license.isBlank(), "license = " + license);

        List<Config> mods = config.get("mods");
        check(mods != null && !mods.isEmpty(), "[[mods]] 不为空");
        if (mods == null) {
            finish();
            return;
        }
        for (Config mod : mods) {
            String modId = mod.get("modId");
            check(modId != null && VALID_MODID.matcher(modId).matches(), "modId = " + modId + " 符合 " + VALID_MODID);
            String version = mod.get("version");
            check(version != null && VALID_VERSION.matcher(version).matches(), "version = " + version);
            if (forge) {
                String displayTest = mod.getOrElse("displayTest", "MATCH_VERSION");
                check(DISPLAY_TEST.contains(displayTest), "displayTest = " + displayTest);
            }
            Config deps = config.get("dependencies");
            List<Config> list = deps == null ? null : deps.get(List.of(modId));
            check(list != null && !list.isEmpty(), "有 [[dependencies." + modId + "]]");
            if (list == null) {
                continue;
            }
            for (Config dep : list) {
                String depId = dep.get("modId");
                check(depId != null, "依赖 modId = " + depId);
                boolean required;
                if (forge) {
                    Object mandatory = dep.get("mandatory");
                    check(mandatory instanceof Boolean, "依赖写了 mandatory（Forge 1.20.1 必填，缺了直接报错）= " + mandatory);
                    required = Boolean.TRUE.equals(mandatory);
                } else {
                    String type = dep.getOrElse("type", "required").toUpperCase(Locale.ROOT);
                    check(NEO_TYPES.contains(type), "依赖 type = " + type);
                    required = type.equals("REQUIRED");
                    check(!dep.contains("mandatory"), "没有写 NeoForge 已经不认的 mandatory");
                }
                String spec = dep.get("versionRange");
                // 两边省略 versionRange 时的默认值不一样：Forge 是 ""（什么都不匹配），NeoForge 是 " "（全匹配）
                String effective = spec != null ? spec : (forge ? "" : " ");
                VersionRange depRange = VersionRange.createFromVersionSpec(effective);
                String ordering = dep.getOrElse("ordering", "NONE");
                String side = dep.getOrElse("side", "BOTH");
                check(ORDERING.contains(ordering), "依赖 ordering = " + ordering);
                check(SIDE.contains(side), "依赖 side = " + side);
                if ("voicechat".equals(depId)) {
                    String[] svc = forge
                            ? new String[]{"1.20.1-2.6.27", "1.20.1-2.6.0", "1.20.1-2.5.0"}
                            : new String[]{"1.21.1-2.6.27", "1.21.1-2.6.0", "1.21.1-2.5.36"};
                    for (String v : svc) {
                        check(depRange.containsVersion(new DefaultArtifactVersion(v)),
                                "SVC 的模组版本号 " + v + " 满足 versionRange " + (spec == null ? "（省略）" : "\"" + spec + "\""));
                    }
                    check(required, "SVC 是必需依赖（没装 SVC 时服务器会给出明确的报错）");
                }
            }
        }
        finish();
    }

    private static void check(boolean condition, String what) {
        if (condition) {
            ok(what);
        } else {
            fail(what);
        }
    }

    private static void ok(String what) {
        System.out.println("  [通过] " + what);
    }

    private static void fail(String what) {
        failures++;
        System.out.println("  [失败] " + what);
    }

    private static void finish() {
        if (failures > 0) {
            System.out.println("  共 " + failures + " 项失败");
            System.exit(1);
        }
    }
}
