package minedeck.companion.net;

import java.util.regex.Pattern;

/**
 * 指令参数的格式检查（只用 java.*）。拼进 PlayerEngine 指令的参数都要先过这里：物品名、生物种类、玩家名里只有
 * 字母、数字和少数几个符号，不会混进空格或 ;（PlayerEngine 用 ; 串联多条指令），大模型没法借参数塞进清单外的动作。
 */
public final class Args {

    public static final Pattern ITEM = Pattern.compile("[a-z0-9_.\\-]+(:[a-z0-9_./\\-]+)?");
    public static final Pattern MOB = ITEM;
    /** 原版的玩家名是 1–16 个字母、数字、下划线；Floodgate 的基岩版玩家带 . 前缀，离线服的名字可能更长，放宽到 20。 */
    public static final Pattern PLAYER = Pattern.compile("[A-Za-z0-9_.]{1,20}");
    public static final Pattern TAG = Pattern.compile("[A-Za-z0-9_.:\\-]{1,64}");
    public static final Pattern DIM = Pattern.compile("[a-z0-9_.\\-]+:[a-z0-9_./\\-]+");

    public static final int MAX_NAME = 32;
    public static final int MAX_TEXT = 256;
    public static final int MAX_COUNT = 36 * 64;
    public static final int MAX_ATTACK_COUNT = 64;
    public static final int MAX_DURATION = 1800;
    public static final int MAX_RADIUS = 48;
    public static final int DEFAULT_RADIUS = 16;
    public static final int MAX_TIMEOUT = 86400;
    public static final int MIN_TIMEOUT = 5;
    public static final int MAX_COORD = 30_000_000;
    public static final int MAX_Y = 2048;

    private Args() {
    }

    /** 伙伴的名字：1–32 个字，不能有控制字符和 §（格式码），去掉首尾空白后再算。 */
    public static String checkName(String raw) {
        if (raw == null) {
            return null;
        }
        String name = raw.trim();
        if (name.isEmpty() || name.codePointCount(0, name.length()) > MAX_NAME) {
            return null;
        }
        for (int i = 0; i < name.length(); i++) {
            char c = name.charAt(i);
            if (Character.isISOControl(c) || c == '§' || c == ' ' || c == ' ') {
                return null;
            }
        }
        return name;
    }

    /**
     * say 的文本：去掉 § 格式码（§ 和它后面的一个字符）和控制字符（换行、制表符换成空格），再去掉首尾空白；
     * 剩下 1–256 个字才算数，否则返回 null（回 bad_args）。名字和这句话会以「&lt;名字&gt; 文本」出现在聊天栏和日志里，
     * 不能让人借它伪造格式或者多出几行。
     */
    public static String checkText(String raw) {
        if (raw == null) {
            return null;
        }
        StringBuilder sb = new StringBuilder(raw.length());
        for (int i = 0; i < raw.length(); i++) {
            char c = raw.charAt(i);
            if (c == '§') {
                i++; // 连同后面的格式字符一起去掉
                continue;
            }
            if (c == '\n' || c == '\r' || c == '\t' || c == '\u2028' || c == '\u2029') {
                sb.append(' ');
                continue;
            }
            if (Character.isISOControl(c)) {
                continue;
            }
            sb.append(c);
        }
        String text = sb.toString().trim();
        if (text.isEmpty() || text.codePointCount(0, text.length()) > MAX_TEXT) {
            return null;
        }
        return text;
    }

    /** 物品名、生物种类：小写；原版的可以省掉 minecraft:。 */
    public static String normalizeId(String raw) {
        return raw.trim().toLowerCase(java.util.Locale.ROOT);
    }

    public static String withNamespace(String id) {
        return id.indexOf(':') >= 0 ? id : "minecraft:" + id;
    }

    /**
     * 交给 PlayerEngine 的 give 的物品名：原版物品去掉 minecraft:，模组物品照写命名空间。PlayerEngine 1.4 的 give 只认这种
     * 写法（它的目录名、ItemHelper.stripItemName 都是这样），写成 minecraft:oak_log 时找不到这个物品、什么都不给就结束了。
     */
    public static String engineItemName(String id) {
        String p = "minecraft:";
        return id.startsWith(p) && id.length() > p.length() ? id.substring(p.length()) : id;
    }
}
