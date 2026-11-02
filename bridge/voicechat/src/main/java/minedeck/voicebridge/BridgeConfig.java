package minedeck.voicebridge;

import java.io.IOException;
import java.io.StringReader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.NoSuchFileException;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.SecureRandom;
import java.util.Collections;
import java.util.LinkedHashSet;
import java.util.Locale;
import java.util.Properties;
import java.util.Set;
import java.util.UUID;

/**
 * 配置文件 {@code config/minedeck/voicebridge.properties}（相对服务器的工作目录，也就是实例目录）。
 *
 * <p>MineDeck 启动服务器前写好它；文件不存在时模组生成一份关闭状态的模板。模组每 2 秒读一次文件、
 * 和上次应用的内容逐字节比较，变了就重新应用，不用重启服务器。不看修改时间和大小：同一毫秒里连写两次、
 * 或者在修改时间只精确到 2 秒的盘（FAT32）上，按修改时间和大小判断会漏掉改动。
 */
final class BridgeConfig {

    /** 测试用：用系统属性指定别的配置文件路径。 */
    static final String PATH_PROPERTY = "minedeck.voicebridge.config";
    static final int DEFAULT_PORT = 24470;
    static final int MIN_TOKEN_LENGTH = 16;

    final boolean enabled;
    final int port;
    final String token;
    final Set<UUID> excludeIds;
    /** 小写的玩家名。 */
    final Set<String> excludeNames;
    final boolean selftest;
    final boolean bypassGroupIsolation;
    /** 不能开端口的原因；能开（或者本来就没开）时为 null。 */
    final String problem;

    private BridgeConfig(boolean enabled, int port, String token, Set<UUID> excludeIds, Set<String> excludeNames,
                         boolean selftest, boolean bypassGroupIsolation, String problem) {
        this.enabled = enabled;
        this.port = port;
        this.token = token;
        this.excludeIds = excludeIds;
        this.excludeNames = excludeNames;
        this.selftest = selftest;
        this.bypassGroupIsolation = bypassGroupIsolation;
        this.problem = problem;
    }

    /** 什么都不开的配置（读不到文件时用）。 */
    static BridgeConfig disabled() {
        return new BridgeConfig(false, DEFAULT_PORT, "", Collections.<UUID>emptySet(), Collections.<String>emptySet(),
                false, true, null);
    }

    static Path path() {
        String override = System.getProperty(PATH_PROPERTY);
        if (override != null && !override.trim().isEmpty()) {
            return Paths.get(override.trim());
        }
        return Paths.get("config", "minedeck", "voicebridge.properties");
    }

    /** 读文件的全部内容；文件不存在时返回 null。 */
    static byte[] read(Path file) throws IOException {
        try {
            return Files.readAllBytes(file);
        } catch (NoSuchFileException e) {
            return null;
        }
    }

    static BridgeConfig load(Path file) throws IOException {
        return parse(Files.readAllBytes(file));
    }

    static BridgeConfig parse(byte[] bytes) throws IOException {
        String text = new String(bytes, StandardCharsets.UTF_8);
        if (text.startsWith("\uFEFF")) {
            // 记事本另存为"UTF-8 带 BOM"时会有这个字符
            text = text.substring(1);
        }
        Properties p = new Properties();
        p.load(new StringReader(text));

        boolean enabled = bool(p, "enabled", false);
        boolean selftest = bool(p, "selftest", false);
        boolean bypass = bool(p, "bypass_group_isolation", true);
        String token = value(p, "token");
        String problem = null;

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
                problem = "port 必须是 1024 到 65535 之间的数字（现在是 \"" + portText + "\"）";
            } else if (token.length() < MIN_TOKEN_LENGTH) {
                problem = "token 至少要 " + MIN_TOKEN_LENGTH + " 个字符（现在是 " + token.length() + " 个）";
            }
        }

        Set<UUID> ids = new LinkedHashSet<UUID>();
        Set<String> names = new LinkedHashSet<String>();
        for (String item : value(p, "exclude").split("[,，;；\\s]+")) {
            String s = item.trim();
            if (s.isEmpty()) {
                continue;
            }
            UUID id = parseUuid(s);
            if (id != null) {
                ids.add(id);
            } else {
                names.add(s.toLowerCase(Locale.ROOT));
            }
        }
        return new BridgeConfig(enabled, port, token, Collections.unmodifiableSet(ids),
                Collections.unmodifiableSet(names), selftest, bypass, problem);
    }

    /** 文件不存在时写一份模板：端口默认关闭，令牌随机生成。 */
    static void writeTemplate(Path file) throws IOException {
        Path dir = file.toAbsolutePath().getParent();
        if (dir != null) {
            Files.createDirectories(dir);
        }
        String nl = System.lineSeparator();
        StringBuilder sb = new StringBuilder();
        sb.append("# MineDeck 音乐共享桥接模组（minedeck_voicebridge）的配置。").append(nl);
        sb.append("# 通常由 MineDeck 自动写入；也可以手动改，保存后约 2 秒内生效，不用重启服务器。").append(nl);
        sb.append(nl);
        sb.append("# 是否在本机打开音乐共享端口。只监听 127.0.0.1，别的电脑连不进来。").append(nl);
        sb.append("enabled=false").append(nl);
        sb.append("# 端口（1024 到 65535）").append(nl);
        sb.append("port=").append(DEFAULT_PORT).append(nl);
        sb.append("# 共享令牌：发送端握手时要带上它，至少 ").append(MIN_TOKEN_LENGTH).append(" 个字符。").append(nl);
        sb.append("token=").append(randomToken()).append(nl);
        sb.append("# 不推送音乐的玩家，填名字或 UUID，多个用英文逗号隔开。").append(nl);
        sb.append("# 比如房主就在开服的这台电脑上玩，本来就听得到原声，把自己填进来可以避免听到两遍（回声）。").append(nl);
        sb.append("exclude=").append(nl);
        sb.append("# 自测模式：没有广播连接时，持续给所有装了 SVC 的玩家播放测试音。测完记得改回 false。").append(nl);
        sb.append("selftest=false").append(nl);
        sb.append("# 处在\"隔离\"类型语音群组里的玩家是否也能听到音乐。").append(nl);
        sb.append("bypass_group_isolation=true").append(nl);
        Files.write(file, sb.toString().getBytes(StandardCharsets.UTF_8));
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

    boolean excludes(UUID id, String name) {
        if (excludeIds.contains(id)) {
            return true;
        }
        return name != null && excludeNames.contains(name.toLowerCase(Locale.ROOT));
    }

    String describeExclusions() {
        if (excludeIds.isEmpty() && excludeNames.isEmpty()) {
            return "无";
        }
        StringBuilder sb = new StringBuilder();
        for (String n : excludeNames) {
            sb.append(sb.length() == 0 ? "" : "、").append(n);
        }
        for (UUID id : excludeIds) {
            sb.append(sb.length() == 0 ? "" : "、").append(id);
        }
        return sb.toString();
    }

    private static String value(Properties p, String key) {
        String v = p.getProperty(key);
        return v == null ? "" : v.trim();
    }

    private static boolean bool(Properties p, String key, boolean fallback) {
        String v = value(p, key).toLowerCase(Locale.ROOT);
        if (v.equals("true") || v.equals("1") || v.equals("yes") || v.equals("on")) {
            return true;
        }
        if (v.equals("false") || v.equals("0") || v.equals("no") || v.equals("off")) {
            return false;
        }
        return fallback;
    }

    private static UUID parseUuid(String s) {
        String t = s;
        if (t.length() == 32 && t.matches("[0-9a-fA-F]{32}")) {
            t = t.substring(0, 8) + "-" + t.substring(8, 12) + "-" + t.substring(12, 16) + "-"
                    + t.substring(16, 20) + "-" + t.substring(20);
        }
        if (!t.matches("[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}")) {
            return null;
        }
        try {
            return UUID.fromString(t);
        } catch (IllegalArgumentException e) {
            return null;
        }
    }
}
