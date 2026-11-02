package minedeck.companion.net;

import java.io.IOException;
import java.io.StringReader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.NoSuchFileException;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.security.SecureRandom;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Properties;
import java.util.Set;
import java.util.TreeMap;
import java.util.regex.Pattern;

/**
 * 配置文件 config/minedeck/companion.properties（相对服务端的工作目录，和音乐桥接的 voicebridge.properties 同一层）。
 * 每个键的意思见 companion/README.md「协议 v1」的「配置文件」。
 *
 * <p>MineDeck 开服前写好；文件不存在时模组生成一份端口关闭的模板（令牌随机）。服务器运行中改了也会生效：
 * {@link Housekeeper} 每 2 秒读一次文件、和上次应用的内容逐字节比较。这个类只负责解析，对象建好后不再改。
 */
public final class CompanionConfig {

    public static final String RELATIVE_PATH = "config/minedeck/companion.properties";
    public static final int DEFAULT_PORT = 24480;
    public static final int MIN_TOKEN_LENGTH = 16;
    public static final int MAX_TOKEN_LENGTH = 255;
    public static final int DEFAULT_MAX_COMPANIONS = 4;
    public static final int MAX_COMPANIONS = 16;
    public static final int MAX_ZONES = 64;
    public static final int MAX_COORD = 30_000_000;
    public static final List<String> DEFAULT_ATTACK_DENY = Collections.unmodifiableList(java.util.Arrays.asList(
            "minecraft:villager", "minecraft:wandering_trader", "minecraft:iron_golem", "minecraft:snow_golem",
            "minecraft:allay"));

    private static final Pattern ZONE_NAME = Pattern.compile("[A-Za-z0-9_-]{1,32}");
    private static final Pattern DIM = Pattern.compile("[a-z0-9_.-]+:[a-z0-9_./-]+");
    private static final Pattern ENTITY_ID = Pattern.compile("[a-z0-9_.-]+(:[a-z0-9_./-]+)?");
    private static final Pattern TOKEN = Pattern.compile("[\\x21-\\x7e]+");

    public final boolean enabled;
    public final int port;
    public final String token;
    /** 端口打不开的原因（enabled=true 但 port、token 写错）；能开或者本来就没开时为 null。 */
    public final String portProblem;
    public final int maxCompanions;
    /** attack 不许打的生物种类，完整 ID（minecraft:villager）。 */
    public final Set<String> attackDeny;
    public final Zones zones;
    /** 其余写错、没生效的条目（给日志和 config.problems 看）。 */
    public final List<String> problems;

    private CompanionConfig(boolean enabled, int port, String token, String portProblem, int maxCompanions,
                            Set<String> attackDeny, Zones zones, List<String> problems) {
        this.enabled = enabled;
        this.port = port;
        this.token = token;
        this.portProblem = portProblem;
        this.maxCompanions = maxCompanions;
        this.attackDeny = attackDeny;
        this.zones = zones;
        this.problems = problems;
    }

    /** 什么都不开的配置（读不到文件时用）。 */
    public static CompanionConfig disabled() {
        return new CompanionConfig(false, DEFAULT_PORT, "", null, DEFAULT_MAX_COMPANIONS,
                Collections.unmodifiableSet(new LinkedHashSet<String>(DEFAULT_ATTACK_DENY)), Zones.NONE,
                Collections.<String>emptyList());
    }

    public static Path path(Path gameDir) {
        return gameDir.resolve(RELATIVE_PATH).toAbsolutePath().normalize();
    }

    /** 读文件的全部内容；文件不存在时返回 null。 */
    public static byte[] read(Path file) throws IOException {
        try {
            return Files.readAllBytes(file);
        } catch (NoSuchFileException e) {
            return null;
        }
    }

    public static CompanionConfig parse(byte[] bytes) throws IOException {
        String text = new String(bytes, StandardCharsets.UTF_8);
        if (text.startsWith("﻿")) {
            // 记事本另存为"UTF-8 带 BOM"时会有这个字符
            text = text.substring(1);
        }
        Properties p = new Properties();
        p.load(new StringReader(text));
        List<String> problems = new ArrayList<String>();

        boolean enabled = bool(p, "enabled", false, problems);
        String token = value(p, "token");
        String portProblem = null;
        int port = DEFAULT_PORT;
        String portText = value(p, "port");
        if (!portText.isEmpty()) {
            try {
                port = Integer.parseInt(portText);
            } catch (NumberFormatException e) {
                port = -1;
            }
        }
        if (enabled) {
            if (port < 1024 || port > 65535) {
                portProblem = "port 必须是 1024 到 65535 之间的数字（现在是 \"" + portText + "\"）";
            } else if (token.length() < MIN_TOKEN_LENGTH || token.length() > MAX_TOKEN_LENGTH
                    || !TOKEN.matcher(token).matches()) {
                portProblem = "token 要有 " + MIN_TOKEN_LENGTH + "–" + MAX_TOKEN_LENGTH
                        + " 个可见的 ASCII 字符（现在是 " + token.length() + " 个）";
            }
        }

        int max = DEFAULT_MAX_COMPANIONS;
        String maxText = value(p, "max_companions");
        if (!maxText.isEmpty()) {
            try {
                max = Integer.parseInt(maxText);
                if (max < 1 || max > MAX_COMPANIONS) {
                    problems.add("max_companions 要在 1–" + MAX_COMPANIONS + " 之间（现在是 " + maxText + "），用默认的 "
                            + DEFAULT_MAX_COMPANIONS);
                    max = DEFAULT_MAX_COMPANIONS;
                }
            } catch (NumberFormatException e) {
                problems.add("max_companions 不是数字：" + maxText + "，用默认的 " + DEFAULT_MAX_COMPANIONS);
                max = DEFAULT_MAX_COMPANIONS;
            }
        }

        Set<String> deny = new LinkedHashSet<String>();
        if (p.getProperty("attack_deny") == null) {
            deny.addAll(DEFAULT_ATTACK_DENY);
        } else {
            for (String item : value(p, "attack_deny").split("[,，;；\\s]+")) {
                String s = item.trim().toLowerCase(Locale.ROOT);
                if (s.isEmpty()) {
                    continue;
                }
                if (!ENTITY_ID.matcher(s).matches()) {
                    problems.add("attack_deny 里的 \"" + item.trim() + "\" 不是生物种类 ID，跳过");
                    continue;
                }
                deny.add(s.contains(":") ? s : "minecraft:" + s);
            }
        }

        // 按名字排序，同样的文件每次得到同样的顺序
        Map<String, String> zoneLines = new TreeMap<String, String>();
        for (String key : p.stringPropertyNames()) {
            if (key.startsWith("nodig.")) {
                zoneLines.put(key, value(p, key));
            } else if (!KNOWN_KEYS.contains(key)) {
                problems.add("不认识的键 " + key + "，忽略");
            }
        }
        List<Zones.Zone> zones = new ArrayList<Zones.Zone>();
        for (Map.Entry<String, String> e : zoneLines.entrySet()) {
            String name = e.getKey().substring("nodig.".length());
            Zones.Zone zone = parseZone(name, e.getValue(), problems);
            if (zone == null) {
                continue;
            }
            if (zones.size() >= MAX_ZONES) {
                problems.add("禁挖区域最多 " + MAX_ZONES + " 个，nodig." + name + " 没有生效");
                continue;
            }
            zones.add(zone);
        }

        return new CompanionConfig(enabled, port, token, portProblem, max,
                Collections.unmodifiableSet(deny), new Zones(zones), Collections.unmodifiableList(problems));
    }

    private static final Set<String> KNOWN_KEYS = new LinkedHashSet<String>(java.util.Arrays.asList(
            "enabled", "port", "token", "max_companions", "attack_deny"));

    /** 「维度 x1 y1 z1 x2 y2 z2」，空格或逗号隔开；写错时把原因加进 problems，返回 null。 */
    static Zones.Zone parseZone(String name, String text, List<String> problems) {
        String where = "nodig." + name;
        if (!ZONE_NAME.matcher(name).matches()) {
            problems.add(where + "：名字只能有 1–32 个字母、数字、_、-，没有生效");
            return null;
        }
        String[] parts = text.trim().split("[,，\\s]+");
        if (parts.length != 7) {
            problems.add(where + "：要写成「维度 x1 y1 z1 x2 y2 z2」（现在是 \"" + text + "\"），没有生效");
            return null;
        }
        String dim = parts[0].toLowerCase(Locale.ROOT);
        if (!DIM.matcher(dim).matches()) {
            problems.add(where + "：维度要写完整的 ID，比如 minecraft:overworld（现在是 \"" + parts[0] + "\"），没有生效");
            return null;
        }
        int[] c = new int[6];
        for (int i = 0; i < 6; i++) {
            try {
                c[i] = Integer.parseInt(parts[i + 1]);
            } catch (NumberFormatException e) {
                problems.add(where + "：坐标要是整数（\"" + parts[i + 1] + "\"），没有生效");
                return null;
            }
            if (Math.abs(c[i]) > MAX_COORD) {
                problems.add(where + "：坐标超出范围（" + c[i] + "），没有生效");
                return null;
            }
        }
        return new Zones.Zone(name, dim, c[0], c[1], c[2], c[3], c[4], c[5]);
    }

    /** 文件不存在时写一份模板：端口默认关闭，令牌随机生成。先写临时文件再改名。 */
    public static void writeTemplate(Path file) throws IOException {
        Path dir = file.toAbsolutePath().getParent();
        if (dir != null) {
            Files.createDirectories(dir);
        }
        String nl = System.lineSeparator();
        StringBuilder sb = new StringBuilder();
        sb.append("# MineDeck AI 伙伴模组（minedeck_companion）的配置。").append(nl);
        sb.append("# 通常由 MineDeck 自动写入；也可以手动改，保存后约 2 秒内生效，不用重启服务器。").append(nl);
        sb.append("# 每一项的说明见模组的 README（companion/README.md 的「协议 v1」→「配置文件」）。").append(nl);
        sb.append(nl);
        sb.append("# 是否在本机打开伙伴的控制端口。只监听 127.0.0.1，别的电脑连不进来。").append(nl);
        sb.append("enabled=false").append(nl);
        sb.append("# 端口（1024 到 65535）").append(nl);
        sb.append("port=").append(DEFAULT_PORT).append(nl);
        sb.append("# 控制令牌：MineDeck 握手时要带上它，至少 ").append(MIN_TOKEN_LENGTH).append(" 个字符。").append(nl);
        sb.append("token=").append(randomToken()).append(nl);
        sb.append("# 同时最多几个伙伴（1 到 ").append(MAX_COMPANIONS).append("）").append(nl);
        sb.append("max_companions=").append(DEFAULT_MAX_COMPANIONS).append(nl);
        sb.append("# 伙伴不许打的生物种类，逗号分隔。玩家、伙伴、有主人的、起了名字的、拴着的生物永远不打。").append(nl);
        sb.append("attack_deny=").append(String.join(",", DEFAULT_ATTACK_DENY)).append(nl);
        sb.append("# 禁挖区域：伙伴不拆、不放这些区域里的方块。格式：nodig.名字=维度 x1 y1 z1 x2 y2 z2").append(nl);
        sb.append("# 比如：nodig.base=minecraft:overworld -120 -64 -80 -40 320 10").append(nl);
        Path tmp = file.resolveSibling(file.getFileName() + ".tmp");
        Files.write(tmp, sb.toString().getBytes(StandardCharsets.UTF_8));
        try {
            Files.move(tmp, file, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE);
        } catch (IOException e) {
            Files.move(tmp, file, StandardCopyOption.REPLACE_EXISTING);
        }
    }

    static String randomToken() {
        byte[] b = new byte[16];
        new SecureRandom().nextBytes(b);
        char[] hex = "0123456789abcdef".toCharArray();
        StringBuilder sb = new StringBuilder();
        for (byte x : b) {
            sb.append(hex[(x >> 4) & 0xF]).append(hex[x & 0xF]);
        }
        return sb.toString();
    }

    /** 给日志看的一行概要。 */
    public String describe() {
        StringBuilder sb = new StringBuilder();
        sb.append("端口 ").append(enabled ? (portProblem == null ? "127.0.0.1:" + port : "打不开（" + portProblem + "）") : "关闭");
        sb.append("；伙伴上限 ").append(maxCompanions);
        sb.append("；禁挖区域 ").append(zones.all().size()).append(" 个");
        for (Zones.Zone z : zones.all()) {
            sb.append(" ").append(z);
        }
        if (!problems.isEmpty()) {
            sb.append("；有 ").append(problems.size()).append(" 处没生效：").append(String.join("；", problems));
        }
        return sb.toString();
    }

    /** 和 JSON 相关的字段，给 WELCOME、status、config_applied 用（键的顺序固定）。 */
    public Map<String, Object> summary() {
        Map<String, Object> m = new LinkedHashMap<String, Object>();
        m.put("maxCompanions", maxCompanions);
        m.put("attackDeny", new ArrayList<String>(attackDeny));
        List<Map<String, Object>> zl = new ArrayList<Map<String, Object>>();
        for (Zones.Zone z : zones.all()) {
            Map<String, Object> zm = new LinkedHashMap<String, Object>();
            zm.put("name", z.name);
            zm.put("dim", z.dim);
            zm.put("min", new int[] {z.minX, z.minY, z.minZ});
            zm.put("max", new int[] {z.maxX, z.maxY, z.maxZ});
            zl.add(zm);
        }
        m.put("zones", zl);
        m.put("problems", new ArrayList<String>(problems));
        return m;
    }

    private static String value(Properties p, String key) {
        String v = p.getProperty(key);
        return v == null ? "" : v.trim();
    }

    private static boolean bool(Properties p, String key, boolean fallback, List<String> problems) {
        String raw = value(p, key);
        String v = raw.toLowerCase(Locale.ROOT);
        if (v.equals("true") || v.equals("1") || v.equals("yes") || v.equals("on")) {
            return true;
        }
        if (v.equals("false") || v.equals("0") || v.equals("no") || v.equals("off")) {
            return false;
        }
        if (!v.isEmpty()) {
            problems.add(key + " 只能是 true 或 false（现在是 \"" + raw + "\"），当成 " + fallback);
        }
        return fallback;
    }
}
