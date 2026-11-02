package minedeck.voicebridge;

import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;

/**
 * MineDeck 和模组之间的本机 TCP 协议（第 1 版）。完整说明见 README 的「协议」一节。
 *
 * <p>每帧：类型 1 字节 | 保留 1 字节（填 0，收到时忽略） | 内容长度 2 字节 | 内容。
 * 所有整数都是小端（little-endian），PCM 采样也是。
 */
final class Protocol {

    static final int VERSION = 1;

    static final int SAMPLE_RATE = 48000;
    static final int FRAME_SAMPLES = 960;
    static final int CHANNELS = 1;

    static final int HEADER_SIZE = 4;
    static final int MAX_PAYLOAD = 4096;
    /** PCM 帧的内容：序号 4 字节 + 960 个 16 位采样。 */
    static final int PCM_PAYLOAD = 4 + FRAME_SAMPLES * 2;

    static final int HELLO = 0x01;
    static final int WELCOME = 0x02;
    static final int ERROR = 0x03;
    static final int PCM = 0x10;
    static final int PING = 0x20;
    static final int PONG = 0x21;
    static final int STATUS = 0x30;
    static final int END = 0x40;

    static final int E_BAD_TOKEN = 1;
    static final int E_VERSION = 2;
    static final int E_BUSY = 3;
    static final int E_PROTOCOL = 4;
    static final int E_DISABLED = 5;

    static final int SOURCE_NONE = 0;
    static final int SOURCE_BROADCAST = 1;
    static final int SOURCE_SELFTEST = 2;

    private Protocol() {
    }

    static final class Frame {
        final int type;
        final byte[] payload;

        Frame(int type, byte[] payload) {
            this.type = type;
            this.payload = payload;
        }
    }

    /** 协议错误：对方发来的数据不合规矩，回 ERROR 后断开。 */
    static final class ProtocolException extends IOException {
        private static final long serialVersionUID = 1L;

        ProtocolException(String message) {
            super(message);
        }
    }

    /** 读一帧。对方在帧与帧之间正常关闭连接时返回 null。 */
    static Frame read(InputStream in) throws IOException {
        byte[] header = new byte[HEADER_SIZE];
        if (!readFully(in, header, true)) {
            return null;
        }
        int type = header[0] & 0xFF;
        int length = u16(header, 2);
        if (length > MAX_PAYLOAD) {
            throw new ProtocolException("帧太长：" + length + " 字节，最多 " + MAX_PAYLOAD);
        }
        byte[] payload = new byte[length];
        readFully(in, payload, false);
        return new Frame(type, payload);
    }

    /** 把一帧编码成字节。 */
    static byte[] encode(int type, byte[] payload) {
        if (payload == null) {
            payload = new byte[0];
        }
        if (payload.length > MAX_PAYLOAD) {
            throw new IllegalArgumentException("帧太长：" + payload.length);
        }
        byte[] out = new byte[HEADER_SIZE + payload.length];
        out[0] = (byte) type;
        out[1] = 0;
        putU16(out, 2, payload.length);
        System.arraycopy(payload, 0, out, HEADER_SIZE, payload.length);
        return out;
    }

    static byte[] errorPayload(int code, String message) {
        byte[] text = utf8(message);
        byte[] out = new byte[2 + text.length];
        putU16(out, 0, code);
        System.arraycopy(text, 0, out, 2, text.length);
        return out;
    }

    /** 从 PCM 帧内容里取出 960 个采样（小端 16 位）。 */
    static short[] pcmSamples(byte[] payload) {
        short[] samples = new short[FRAME_SAMPLES];
        ByteBuffer.wrap(payload, 4, FRAME_SAMPLES * 2).order(ByteOrder.LITTLE_ENDIAN).asShortBuffer().get(samples);
        return samples;
    }

    static int u16(byte[] b, int offset) {
        return (b[offset] & 0xFF) | (b[offset + 1] & 0xFF) << 8;
    }

    static long u32(byte[] b, int offset) {
        return ((long) (b[offset] & 0xFF))
                | ((long) (b[offset + 1] & 0xFF)) << 8
                | ((long) (b[offset + 2] & 0xFF)) << 16
                | ((long) (b[offset + 3] & 0xFF)) << 24;
    }

    static void putU16(byte[] b, int offset, int value) {
        b[offset] = (byte) value;
        b[offset + 1] = (byte) (value >>> 8);
    }

    static void putU32(byte[] b, int offset, long value) {
        b[offset] = (byte) value;
        b[offset + 1] = (byte) (value >>> 8);
        b[offset + 2] = (byte) (value >>> 16);
        b[offset + 3] = (byte) (value >>> 24);
    }

    static byte[] utf8(String s) {
        return s == null ? new byte[0] : s.getBytes(StandardCharsets.UTF_8);
    }

    static String utf8(byte[] b, int offset, int length) {
        return new String(b, offset, length, StandardCharsets.UTF_8);
    }

    /** 读满 buffer。allowCleanEof 为真且一个字节都没读到就遇到 EOF 时返回 false。 */
    private static boolean readFully(InputStream in, byte[] buffer, boolean allowCleanEof) throws IOException {
        int read = 0;
        while (read < buffer.length) {
            int n = in.read(buffer, read, buffer.length - read);
            if (n < 0) {
                if (read == 0 && allowCleanEof) {
                    return false;
                }
                throw new EOFException("连接在帧中间断开");
            }
            read += n;
        }
        return true;
    }
}
