package minedeck.voicebridge;

import java.io.BufferedInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.SocketTimeoutException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

/**
 * 在 127.0.0.1 上监听，接收 MineDeck（或 testsend）的广播连接。同一时间只接受一个广播连接。
 *
 * <p>发给对方的帧（WELCOME、ERROR、PONG、STATUS、END）都放进这个连接自己的发送队列，由它的发送线程去写：
 * 别的线程（定时检查、停服）调 {@link Connection#sendStatus()}、{@link Connection#end(String)} 从不会卡在
 * socket 上。对方一直不读数据时 socket 写满，发送线程的一次写入卡住超过 5 秒（或者回 PONG 时 5 秒都排不进队列），
 * 就断开这个连接。
 *
 * <p>{@link #close(String)} 之后这个对象就作废了：再调 {@link #apply(BridgeConfig)}、{@link #tick()} 什么都不做，
 * 不会在停服之后又把端口打开（定时检查刚读完配置、语音服务器就停了，两边会撞在一起）。
 */
final class ControlServer {

    static final int HANDSHAKE_TIMEOUT_MS = 5000;
    /** 5 秒没收到任何帧（PCM 或 PING）就认为对方掉线。 */
    static final int IDLE_TIMEOUT_MS = 5000;
    static final int MAX_CONNECTIONS = 4;
    static final long BIND_RETRY_MS = 10_000L;
    /** 发送队列最多攒这么多帧（PONG 每秒一个、STATUS 每 2 秒一个，正常时几乎是空的）。 */
    static final int OUTBOX_FRAMES = 64;
    /** 一次写入卡住（对方不读数据，socket 写满）超过这么久就断开。 */
    static final long WRITE_STALL_MS = 5000L;
    /** 主动结束连接时，最多等 END 发出去这么久，然后不管发没发出去都断开。 */
    static final long END_FLUSH_MS = 500L;

    private final AudioEngine engine;
    private final Listeners listeners;

    private ServerSocket server;
    private int boundPort = -1;
    private int wantedPort = -1;
    private long nextBindAttempt;
    private int bindFailures;
    private volatile String token = "";
    /** 端口开着、还在接受新的广播连接。 */
    private volatile boolean accepting;
    /** close() 过了：之后什么都不再打开。 */
    private boolean closed;

    private final AtomicReference<Connection> active = new AtomicReference<Connection>();
    private final AtomicInteger open = new AtomicInteger();
    private final AtomicInteger counter = new AtomicInteger();

    ControlServer(AudioEngine engine, Listeners listeners) {
        this.engine = engine;
        this.listeners = listeners;
    }

    /** 按配置开关端口。启动时和配置文件变了时调用。 */
    synchronized void apply(BridgeConfig config) {
        if (closed) {
            return;
        }
        token = config.token;
        if (!config.enabled || config.problem != null) {
            if (config.enabled) {
                Log.warn("配置有问题，音乐共享端口不打开：" + config.problem);
            }
            wantedPort = -1;
            if (server != null) {
                closeServer();
                Log.info("音乐共享端口已关闭（" + (config.enabled ? "配置有问题" : "配置里 enabled=false") + "）");
            }
            Connection c = active.get();
            if (c != null) {
                c.end("音乐共享已在配置里关闭");
            }
            return;
        }
        wantedPort = config.port;
        if (server != null && boundPort == config.port) {
            return;
        }
        if (server != null) {
            closeServer();
            Connection c = active.get();
            if (c != null) {
                c.end("端口改了，请重新连接");
            }
        }
        nextBindAttempt = 0;
        bindFailures = 0;
        tryBind();
    }

    /** 每 2 秒调用（定时检查线程）：端口被占用时重试，给广播连接发 STATUS（只是放进发送队列，不会卡住）。 */
    void tick() {
        synchronized (this) {
            if (closed) {
                return;
            }
            if (server == null && wantedPort > 0 && System.currentTimeMillis() >= nextBindAttempt) {
                tryBind();
            }
        }
        Connection c = active.get();
        if (c != null) {
            c.sendStatus();
        }
    }

    synchronized void close(String reason) {
        closed = true;
        wantedPort = -1;
        closeServer();
        Connection c = active.get();
        if (c != null) {
            c.end(reason);
        }
    }

    boolean hasBroadcast() {
        return active.get() != null;
    }

    private void tryBind() {
        int port = wantedPort;
        try {
            ServerSocket s = new ServerSocket();
            try {
                s.bind(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), port), MAX_CONNECTIONS);
            } catch (IOException e) {
                s.close();
                throw e;
            }
            server = s;
            boundPort = port;
            accepting = true;
            Thread t = new Thread(new Acceptor(s), "MineDeck-VoiceBridge-Accept");
            t.setDaemon(true);
            t.start();
            Log.info("音乐共享端口已打开：127.0.0.1:" + port + "（只接受本机连接，同一时间只接受一个广播连接）"
                    + (bindFailures > 0 ? "，之前失败了 " + bindFailures + " 次" : ""));
            bindFailures = 0;
        } catch (IOException e) {
            nextBindAttempt = System.currentTimeMillis() + BIND_RETRY_MS;
            // 第一次失败就说，之后每 5 分钟提醒一次，免得刷屏
            if (bindFailures++ % 30 == 0) {
                Log.warn("打不开端口 127.0.0.1:" + port + "（" + e.getMessage() + "），每 10 秒重试一次。"
                        + "端口可能被别的程序占用了，一直失败的话请换一个端口。");
            }
        }
    }

    private void closeServer() {
        accepting = false;
        if (server != null) {
            try {
                server.close();
            } catch (IOException ignored) {
                // 已经关了
            }
        }
        server = null;
        boundPort = -1;
    }

    private final class Acceptor implements Runnable {
        private final ServerSocket socket;

        Acceptor(ServerSocket socket) {
            this.socket = socket;
        }

        @Override
        public void run() {
            while (!socket.isClosed()) {
                Socket s;
                try {
                    s = socket.accept();
                } catch (IOException e) {
                    if (!socket.isClosed()) {
                        Log.error("接受连接时出错", e);
                    }
                    return;
                }
                if (open.incrementAndGet() > MAX_CONNECTIONS) {
                    open.decrementAndGet();
                    closeQuietly(s);
                    continue;
                }
                Connection c = new Connection(s, counter.incrementAndGet());
                Thread t = new Thread(c, "MineDeck-VoiceBridge-Conn-" + c.id);
                t.setDaemon(true);
                t.start();
            }
        }
    }

    /** 一个 TCP 连接：先握手，成为广播连接后不停地读帧；发出去的帧由它自己的发送线程写。 */
    private final class Connection implements Runnable {
        final Socket socket;
        final int id;
        private OutputStream out;
        private String name = "";
        private boolean broadcasting;
        private long lastSeq = -1;
        private long gaps;
        private boolean warnedUnknown;
        private volatile String endReason;

        /** 发送队列；CLOSE 表示发完前面的帧就断开。 */
        private final BlockingQueue<byte[]> outbox = new ArrayBlockingQueue<byte[]>(OUTBOX_FRAMES + 2);
        private final CountDownLatch flushed = new CountDownLatch(1);
        private volatile Thread sender;
        /** 发送线程正在写的那一帧是什么时候开始写的（System.nanoTime）；没在写时为 0。 */
        private volatile long writingSince;

        Connection(Socket socket, int id) {
            this.socket = socket;
            this.id = id;
        }

        @Override
        public void run() {
            String reason = "对方关闭了连接";
            try {
                socket.setSoTimeout(HANDSHAKE_TIMEOUT_MS);
                socket.setTcpNoDelay(true);
                InputStream in = new BufferedInputStream(socket.getInputStream());
                out = socket.getOutputStream();
                startSender();
                if (!handshake(in)) {
                    return;
                }
                socket.setSoTimeout(IDLE_TIMEOUT_MS);
                reason = readLoop(in);
            } catch (SocketTimeoutException e) {
                reason = broadcasting ? "心跳超时（" + IDLE_TIMEOUT_MS / 1000 + " 秒没收到数据）" : "握手超时";
            } catch (Protocol.ProtocolException e) {
                reason = "协议错误：" + e.getMessage();
                sendError(Protocol.E_PROTOCOL, e.getMessage());
            } catch (IOException e) {
                reason = endReason != null ? endReason : "连接断开（" + e.getMessage() + "）";
            } catch (Throwable t) {
                reason = "内部错误：" + t;
                Log.error("处理广播连接时出错", t);
            } finally {
                closeQuietly(socket);
                Thread t = sender;
                if (t != null) {
                    t.interrupt();
                }
                open.decrementAndGet();
                if (broadcasting) {
                    active.compareAndSet(this, null);
                    engine.sessionEnded();
                    Log.info("广播连接已断开：" + displayName() + "，原因：" + reason
                            + "；本次共收到 " + engine.session.received.get() + " 帧"
                            + (gaps > 0 ? "，序号不连续 " + gaps + " 次" : ""));
                }
            }
        }

        private boolean handshake(InputStream in) throws IOException {
            Protocol.Frame hello = Protocol.read(in);
            if (hello == null) {
                return false;
            }
            if (hello.type != Protocol.HELLO) {
                sendError(Protocol.E_PROTOCOL, "第一帧必须是 HELLO");
                return false;
            }
            byte[] p = hello.payload;
            if (p.length < 3) {
                sendError(Protocol.E_PROTOCOL, "HELLO 太短");
                return false;
            }
            int version = Protocol.u16(p, 0);
            int tokenLength = p[2] & 0xFF;
            if (p.length < 3 + tokenLength) {
                sendError(Protocol.E_PROTOCOL, "HELLO 里的令牌长度不对");
                return false;
            }
            String presented = Protocol.utf8(p, 3, tokenLength);
            name = Protocol.utf8(p, 3 + tokenLength, p.length - 3 - tokenLength).trim();
            if (version != Protocol.VERSION) {
                sendError(Protocol.E_VERSION, "协议版本不支持：收到 " + version + "，模组支持 " + Protocol.VERSION);
                Log.warn("拒绝广播连接 #" + id + "：协议版本 " + version + " 不支持");
                return false;
            }
            String expected = token;
            if (expected.isEmpty() || !MessageDigest.isEqual(
                    expected.getBytes(StandardCharsets.UTF_8), presented.getBytes(StandardCharsets.UTF_8))) {
                sleepQuietly(300);
                sendError(Protocol.E_BAD_TOKEN, "令牌不对");
                Log.warn("拒绝广播连接 #" + id + "（" + displayName() + "）：令牌不对");
                return false;
            }
            if (!accepting) {
                sendError(Protocol.E_DISABLED, "音乐共享已关闭");
                return false;
            }
            if (!active.compareAndSet(null, this)) {
                Connection other = active.get();
                sendError(Protocol.E_BUSY, "已经有一个广播连接："
                        + (other != null ? other.displayName() : "未知") + "，请等它结束");
                Log.warn("拒绝广播连接 #" + id + "（" + displayName() + "）：已经有一个广播连接");
                return false;
            }
            broadcasting = true;
            engine.sessionStarted();

            byte[] version8 = Protocol.utf8(BridgePlugin.VERSION);
            byte[] welcome = new byte[11 + version8.length];
            Protocol.putU16(welcome, 0, Protocol.VERSION);
            Protocol.putU32(welcome, 2, Protocol.SAMPLE_RATE);
            Protocol.putU16(welcome, 6, Protocol.FRAME_SAMPLES);
            welcome[8] = (byte) Protocol.CHANNELS;
            Protocol.putU16(welcome, 9, Math.min(listeners.count(), 0xFFFF));
            System.arraycopy(version8, 0, welcome, 11, version8.length);
            outbox.offer(Protocol.encode(Protocol.WELCOME, welcome));
            Log.info("广播连接已建立：" + displayName() + "（#" + id + "，来自 " + socket.getRemoteSocketAddress()
                    + "），当前听众 " + listeners.count() + " 位");
            return true;
        }

        private String readLoop(InputStream in) throws IOException {
            while (true) {
                Protocol.Frame f = Protocol.read(in);
                if (f == null) {
                    return endReason != null ? endReason : "对方关闭了连接";
                }
                switch (f.type) {
                    case Protocol.PCM:
                        if (f.payload.length != Protocol.PCM_PAYLOAD) {
                            throw new Protocol.ProtocolException("PCM 帧长度应为 " + Protocol.PCM_PAYLOAD
                                    + " 字节（序号 4 + 960 个 16 位采样），收到 " + f.payload.length);
                        }
                        long seq = Protocol.u32(f.payload, 0);
                        if (lastSeq >= 0 && seq != ((lastSeq + 1) & 0xFFFFFFFFL)) {
                            gaps++;
                        }
                        lastSeq = seq;
                        engine.offer(Protocol.pcmSamples(f.payload));
                        break;
                    case Protocol.PING:
                        pong(f.payload);
                        break;
                    case Protocol.END:
                        String why = Protocol.utf8(f.payload, 0, f.payload.length).trim();
                        return "对方结束了广播" + (why.isEmpty() ? "" : "（" + why + "）");
                    default:
                        if (!warnedUnknown) {
                            warnedUnknown = true;
                            Log.warn("广播连接发来了不认识的帧类型 0x" + Integer.toHexString(f.type) + "，已忽略");
                        }
                        break;
                }
            }
        }

        /** 定时检查线程每 2 秒调用：发 STATUS（只排进队列，排不进就这次不发），顺便看发送线程是不是卡住了。 */
        void sendStatus() {
            long since = writingSince;
            if (since != 0 && System.nanoTime() - since > TimeUnit.MILLISECONDS.toNanos(WRITE_STALL_MS)) {
                stalled();
                return;
            }
            byte[] p = new byte[25];
            Protocol.putU16(p, 0, Math.min(listeners.count(), 0xFFFF));
            Protocol.putU16(p, 2, Math.min(engine.buffered(), 0xFFFF));
            p[4] = (byte) engine.currentSource();
            Protocol.putU32(p, 5, engine.session.received.get());
            Protocol.putU32(p, 9, engine.session.played.get());
            Protocol.putU32(p, 13, engine.session.sent.get());
            Protocol.putU32(p, 17, engine.session.dropped.get());
            Protocol.putU32(p, 21, engine.session.underruns.get());
            outbox.offer(Protocol.encode(Protocol.STATUS, p));
        }

        /** 主动结束这个连接（关服、关掉共享、改端口）：END 排进发送队列，最多等 0.5 秒让它发出去，然后断开。 */
        void end(String reason) {
            endReason = reason;
            sendLast(Protocol.END, Protocol.utf8(reason));
        }

        private void sendError(int code, String message) {
            sendLast(Protocol.ERROR, Protocol.errorPayload(code, message));
        }

        /** 发最后一帧然后断开：排进发送队列（排不进就算了），最多等 0.5 秒让它发出去。 */
        private void sendLast(int type, byte[] payload) {
            outbox.offer(Protocol.encode(type, payload));
            outbox.offer(CLOSE);
            try {
                flushed.await(END_FLUSH_MS, TimeUnit.MILLISECONDS);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
            closeQuietly(socket);
        }

        /** 回 PONG：只会阻塞这个连接自己的线程；5 秒都排不进队列说明对方一直不读数据。 */
        private void pong(byte[] payload) throws IOException {
            boolean queued;
            try {
                queued = outbox.offer(Protocol.encode(Protocol.PONG, payload), WRITE_STALL_MS, TimeUnit.MILLISECONDS);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new IOException("连接线程被中断");
            }
            if (!queued) {
                stalled();
                throw new IOException(endReason);
            }
        }

        /** 对方一直不读数据：断开这个连接（发送线程卡在 socket 上，END 也发不出去了）。 */
        private void stalled() {
            if (endReason == null) {
                endReason = "对方 " + WRITE_STALL_MS / 1000 + " 秒没读数据，断开";
            }
            closeQuietly(socket);
        }

        private void startSender() {
            Thread t = new Thread(new Runnable() {
                @Override
                public void run() {
                    try {
                        while (true) {
                            byte[] f = outbox.take();
                            if (f == CLOSE) {
                                break;
                            }
                            writingSince = System.nanoTime() | 1; // 0 留给「没在写」
                            out.write(f);
                            out.flush();
                            writingSince = 0;
                        }
                    } catch (IOException e) {
                        // 连接断了；读的那一边也会发现
                    } catch (InterruptedException e) {
                        // 连接结束
                    } finally {
                        flushed.countDown();
                        closeQuietly(socket);
                    }
                }
            }, "MineDeck-VoiceBridge-Send-" + id);
            t.setDaemon(true);
            sender = t;
            t.start();
        }

        private String displayName() {
            return name.isEmpty() ? "未署名的发送端" : "「" + name + "」";
        }
    }

    /** 发送队列里的「发完就断开」标记（按引用比较）。 */
    private static final byte[] CLOSE = new byte[0];

    private static void closeQuietly(Socket s) {
        try {
            s.close();
        } catch (IOException ignored) {
            // 已经关了
        }
    }

    private static void sleepQuietly(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
