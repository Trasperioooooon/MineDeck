package minedeck.companion.net;

import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;

/**
 * MineDeck 和伙伴模组之间的本机 TCP 协议 v1，完整说明见 companion/README.md 的「协议 v1」。
 *
 * <p>帧格式和音乐桥接（bridge/voicechat）一样：类型 1 字节 | 保留 1 字节（填 0，收到时忽略） | 内容长度 2 字节 | 内容，
 * 整数都是小端。指令、回复、事件、状态的内容都是 UTF-8 的 JSON，一帧最长 65535 字节。
 *
 * <p>这个类只用 java.*，NeoForge 1.21.1 版和 Forge 1.20.1 版共用。
 */
public final class Protocol {

    /** 模组支持的最高协议版本。 */
    public static final int VERSION = 1;
    /** 模组支持的最低协议版本。 */
    public static final int MIN_VERSION = 1;

    public static final int HEADER_SIZE = 4;
    public static final int MAX_PAYLOAD = 0xFFFF;

    /** MineDeck → 模组：协议版本 2 | 令牌长度 1 | 令牌 | 客户端名字（剩下的全部）。 */
    public static final int HELLO = 0x01;
    /** 模组 → MineDeck：协议版本 2 | JSON。 */
    public static final int WELCOME = 0x02;
    /** 模组 → MineDeck：错误码 2 | 说明。发完就断开。 */
    public static final int ERROR = 0x03;
    /** MineDeck → 模组：JSON 指令。 */
    public static final int REQUEST = 0x10;
    /** 模组 → MineDeck：JSON 回复，每条指令一条。 */
    public static final int RESPONSE = 0x11;
    /** 模组 → MineDeck：JSON 事件。 */
    public static final int EVENT = 0x12;
    /** MineDeck → 模组：任意内容，模组原样回 PONG。 */
    public static final int PING = 0x20;
    public static final int PONG = 0x21;
    /** 模组 → MineDeck：每 2 秒一次的 JSON 状态。 */
    public static final int STATUS = 0x30;
    /** 双向：原因。发完就断开。 */
    public static final int END = 0x40;

    public static final int E_BAD_TOKEN = 1;
    public static final int E_VERSION = 2;
    public static final int E_BUSY = 3;
    public static final int E_PROTOCOL = 4;
    public static final int E_DISABLED = 5;

    private Protocol() {
    }

    public static final class Frame {
        public final int type;
        public final byte[] payload;

        Frame(int type, byte[] payload) {
            this.type = type;
            this.payload = payload;
        }

        public String text() {
            return new String(payload, StandardCharsets.UTF_8);
        }
    }

    /** 对方发来的数据不合规矩：回 ERROR 后断开。 */
    public static final class ProtocolException extends IOException {
        private static final long serialVersionUID = 1L;

        public ProtocolException(String message) {
            super(message);
        }
    }

    /** 读一帧。对方在帧与帧之间正常关闭连接时返回 null。 */
    public static Frame read(InputStream in) throws IOException {
        byte[] header = new byte[HEADER_SIZE];
        if (!readFully(in, header, true)) {
            return null;
        }
        int type = header[0] & 0xFF;
        int length = u16(header, 2);
        byte[] payload = new byte[length];
        readFully(in, payload, false);
        return new Frame(type, payload);
    }

    /** 编码一帧（帧头 + 内容）。内容超过 65535 字节时抛 IllegalArgumentException。 */
    public static byte[] encode(int type, byte[] payload) {
        if (payload.length > MAX_PAYLOAD) {
            throw new IllegalArgumentException("帧太长：" + payload.length + " 字节，最多 " + MAX_PAYLOAD);
        }
        byte[] buf = new byte[HEADER_SIZE + payload.length];
        buf[0] = (byte) type;
        buf[2] = (byte) payload.length;
        buf[3] = (byte) (payload.length >>> 8);
        System.arraycopy(payload, 0, buf, HEADER_SIZE, payload.length);
        return buf;
    }

    public static byte[] utf8(String s) {
        return s.getBytes(StandardCharsets.UTF_8);
    }

    /** 协议版本 2 字节 + 文本。WELCOME 用。 */
    public static byte[] versionAnd(int version, String text) {
        byte[] t = utf8(text);
        byte[] p = new byte[2 + t.length];
        p[0] = (byte) version;
        p[1] = (byte) (version >>> 8);
        System.arraycopy(t, 0, p, 2, t.length);
        return p;
    }

    /** 错误码 2 字节 + 说明。ERROR 用。 */
    public static byte[] error(int code, String message) {
        byte[] t = utf8(message);
        byte[] p = new byte[2 + t.length];
        p[0] = (byte) code;
        p[1] = (byte) (code >>> 8);
        System.arraycopy(t, 0, p, 2, t.length);
        return p;
    }

    public static int u16(byte[] b, int off) {
        return (b[off] & 0xFF) | (b[off + 1] & 0xFF) << 8;
    }

    private static boolean readFully(InputStream in, byte[] buf, boolean eofOk) throws IOException {
        int n = 0;
        while (n < buf.length) {
            int r = in.read(buf, n, buf.length - n);
            if (r < 0) {
                if (n == 0 && eofOk) {
                    return false;
                }
                throw new EOFException("连接在一帧中间断开");
            }
            n += r;
        }
        return true;
    }
}
