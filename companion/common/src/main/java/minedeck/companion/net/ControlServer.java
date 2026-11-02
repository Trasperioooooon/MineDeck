package minedeck.companion.net;

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

import org.slf4j.Logger;

/**
 * 在 127.0.0.1 上监听 MineDeck（或 testctl）的控制连接，协议见 companion/README.md 的「协议 v1」。
 * 同一时间只接受一个控制连接。
 *
 * <p>网络线程只负责收发和握手：收到的指令交给 {@link Handler}，由它切到服务器主线程去执行。发给对方的帧都放进这个连接
 * 自己的发送队列，由它的发送线程去写，别的线程（主线程、定时检查线程）从不卡在 socket 上；对方一直不读数据、一次写入
 * 卡住超过 5 秒，就断开这个连接。
 *
 * <p>端口按配置开关（{@link #apply}），配置变了随时调。{@link #close} 之后这个对象就作废了：再调 {@link #apply}、
 * {@link #tick} 什么都不做，不会在关服之后又把端口打开（定时检查刚读完配置、服务器就开始关了，两边会撞在一起）。
 */
public final class ControlServer {

    public interface Handler {
        /** 网络线程上调用。实现要切到服务器主线程执行，并且每条指令都要回一条 RESPONSE。 */
        void onRequest(String json, Connection conn);

        /** WELCOME 里的 JSON。在服务器主线程上调用（经 {@link #onMainThread}），读这一刻的状态。 */
        String welcomeJson(int version);

        /** 每 2 秒一次的 STATUS。定时检查线程上调用，要线程安全（读快照）。 */
        String statusJson();

        /** 把 task 交给服务器主线程执行（任意线程上调用，不等它做完）。 */
        void onMainThread(Runnable task);
    }

    static final int HANDSHAKE_TIMEOUT_MS = 5000;
    /** 10 秒没收到任何帧就认为对方掉线（MineDeck 每 2 秒发一次 PING）。 */
    static final int IDLE_TIMEOUT_MS = 10000;
    static final int MAX_SOCKETS = 4;
    static final long BIND_RETRY_MS = 10_000L;
    static final int OUTBOX_FRAMES = 1024;
    /** 一次写入卡住（对方不读数据，socket 写满）超过这么久就断开。 */
    static final long WRITE_STALL_MS = 5000L;
    /** 主动结束连接时，最多等 END 发出去这么久，然后不管发没发出去都断开。 */
    static final long END_FLUSH_MS = 500L;
    /** 令牌不对时等这么久再回 ERROR，免得被拿来猜令牌。 */
    static final long BAD_TOKEN_DELAY_MS = 300L;
    /** 握手时最多等服务器主线程这么久来生成 WELCOME（MineDeck 等 WELCOME 等 6 秒）；等不到回 ERROR 5。 */
    static final long WELCOME_WAIT_MS = 4000L;

    private final Handler handler;
    private final Logger log;

    private ServerSocket server;
    private int boundPort = -1;
    private int wantedPort = -1;
    private long nextBindAttempt;
    private int bindFailures;
    private volatile String token = "";
    /** 端口开着、还在接受新的控制连接。 */
    private volatile boolean accepting;
    /** close() 过了：之后什么都不再打开。 */
    private boolean closed;

    private final AtomicReference<Connection> active = new AtomicReference<Connection>();
    private final AtomicInteger open = new AtomicInteger();
    private final AtomicInteger counter = new AtomicInteger();

    public ControlServer(Handler handler, Logger log) {
        this.handler = handler;
        this.log = log;
    }

    /** 按配置开关端口。开服时和配置文件变了时调用。 */
    public synchronized void apply(CompanionConfig config) {
        if (closed) {
            return;
        }
        token = config.token;
        if (!config.enabled || config.portProblem != null) {
            if (config.enabled) {
                log.warn("伙伴配置有问题，控制端口不打开：{}", config.portProblem);
            }
            wantedPort = -1;
            if (server != null) {
                closeServer();
                log.info("伙伴控制端口已关闭（{}）", config.enabled ? "配置有问题" : "配置里 enabled=false");
            }
            Connection c = active.get();
            if (c != null) {
                c.end("伙伴控制端口已在配置里关闭");
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

    /** 每 2 秒调用（定时检查线程）：端口被占用时重试，给控制连接发 STATUS（只是放进发送队列，不会卡住）。 */
    public void tick() {
        synchronized (this) {
            if (closed) {
                return;
            }
            if (server == null && wantedPort > 0 && System.currentTimeMillis() >= nextBindAttempt) {
                tryBind();
            }
        }
        Connection c = active.get();
        if (c != null && c.ready) {
            if (c.stalled()) {
                return;
            }
            try {
                c.send(Protocol.STATUS, Protocol.utf8(handler.statusJson()));
            } catch (RuntimeException e) {
                log.warn("生成 STATUS 失败：{}", e.toString());
            }
        }
    }

    public synchronized void close(String reason) {
        closed = true;
        wantedPort = -1;
        closeServer();
        Connection c = active.get();
        if (c != null) {
            c.end(reason);
        }
    }

    /** 给当前的控制连接发一个事件；没有连接时丢掉（MineDeck 重连后从 WELCOME 拿最新状态）。 */
    public void event(String json) {
        Connection c = active.get();
        if (c != null && c.ready) {
            c.send(Protocol.EVENT, Protocol.utf8(json));
        }
    }

    public boolean connected() {
        Connection c = active.get();
        return c != null && c.ready;
    }

    public synchronized int boundPort() {
        return boundPort;
    }

    private void tryBind() {
        int port = wantedPort;
        try {
            ServerSocket s = new ServerSocket();
            try {
                s.bind(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), port), MAX_SOCKETS);
            } catch (IOException e) {
                s.close();
                throw e;
            }
            server = s;
            boundPort = port;
            accepting = true;
            Thread t = new Thread(new Acceptor(s), "MineDeck-Companion-Accept");
            t.setDaemon(true);
            t.start();
            log.info("伙伴控制端口已打开：127.0.0.1:{}（只接受本机连接，同一时间只接受一个控制连接）{}", port,
                    bindFailures > 0 ? "，之前失败了 " + bindFailures + " 次" : "");
            bindFailures = 0;
        } catch (IOException e) {
            nextBindAttempt = System.currentTimeMillis() + BIND_RETRY_MS;
            // 第一次失败就说，之后每 5 分钟提醒一次，免得刷屏
            if (bindFailures++ % 30 == 0) {
                log.warn("打不开伙伴控制端口 127.0.0.1:{}（{}），每 10 秒重试一次。端口可能被别的程序占用了，一直失败的话请换一个端口。",
                        port, e.getMessage());
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
                        log.error("接受连接时出错", e);
                    }
                    return;
                }
                if (open.incrementAndGet() > MAX_SOCKETS) {
                    open.decrementAndGet();
                    closeQuietly(s);
                    continue;
                }
                Connection c = new Connection(s, counter.incrementAndGet());
                Thread t = new Thread(c, "MineDeck-Companion-Conn-" + c.id);
                t.setDaemon(true);
                t.start();
            }
        }
    }

    /** 一个 TCP 连接：先握手，成为控制连接后不停地读帧；发出去的帧由它自己的发送线程写。 */
    public final class Connection implements Runnable {
        final Socket socket;
        final int id;
        private OutputStream out;
        private String client = "";
        /** 握手完成、是当前的控制连接。 */
        volatile boolean ready;
        private volatile boolean controlling;
        private volatile String endReason;
        private boolean warnedUnknown;

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

        public String client() {
            return client;
        }

        /** 回一条 RESPONSE（主线程上调用，只放进发送队列）。 */
        public void response(String json) {
            send(Protocol.RESPONSE, Protocol.utf8(json));
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
                if (controlling) {
                    reason = "心跳超时（" + IDLE_TIMEOUT_MS / 1000 + " 秒没收到任何帧）";
                    end(reason);
                } else {
                    reason = "握手超时";
                }
            } catch (Protocol.ProtocolException e) {
                reason = "协议错误：" + e.getMessage();
                sendLast(Protocol.ERROR, Protocol.error(Protocol.E_PROTOCOL, e.getMessage()));
            } catch (IOException e) {
                reason = endReason != null ? endReason : "连接断开（" + e.getMessage() + "）";
            } catch (Throwable t) {
                reason = "内部错误：" + t;
                log.error("处理控制连接时出错", t);
            } finally {
                closeQuietly(socket);
                Thread t = sender;
                if (t != null) {
                    t.interrupt();
                }
                open.decrementAndGet();
                if (controlling) {
                    ready = false;
                    // 先让出"当前连接"的位置，MineDeck 马上重连时不会被当成"已经有一个控制连接"
                    active.compareAndSet(this, null);
                    log.info("控制连接已断开：{}，原因：{}", displayName(), reason);
                }
            }
        }

        private boolean handshake(InputStream in) throws IOException {
            Protocol.Frame hello = Protocol.read(in);
            if (hello == null) {
                return false;
            }
            if (hello.type != Protocol.HELLO) {
                sendLast(Protocol.ERROR, Protocol.error(Protocol.E_PROTOCOL, "第一帧必须是 HELLO"));
                return false;
            }
            byte[] p = hello.payload;
            if (p.length < 3 || p.length < 3 + (p[2] & 0xFF)) {
                sendLast(Protocol.ERROR, Protocol.error(Protocol.E_PROTOCOL, "HELLO 的格式不对"));
                return false;
            }
            int version = Protocol.u16(p, 0);
            int tokenLength = p[2] & 0xFF;
            byte[] presented = new byte[tokenLength];
            System.arraycopy(p, 3, presented, 0, tokenLength);
            client = new String(p, 3 + tokenLength, p.length - 3 - tokenLength, StandardCharsets.UTF_8).trim();
            int use = Math.min(version, Protocol.VERSION);
            if (use < Protocol.MIN_VERSION) {
                sendLast(Protocol.ERROR, Protocol.error(Protocol.E_VERSION, "协议版本不支持：收到 " + version + "，模组支持 "
                        + Protocol.MIN_VERSION + "–" + Protocol.VERSION));
                log.warn("拒绝控制连接 #{}（{}）：协议版本 {} 不支持", id, displayName(), version);
                return false;
            }
            String expected = token;
            if (expected.isEmpty() || !MessageDigest.isEqual(expected.getBytes(StandardCharsets.UTF_8), presented)) {
                sleepQuietly(BAD_TOKEN_DELAY_MS);
                sendLast(Protocol.ERROR, Protocol.error(Protocol.E_BAD_TOKEN, "令牌不对"));
                log.warn("拒绝控制连接 #{}（{}）：令牌不对", id, displayName());
                return false;
            }
            if (!accepting) {
                sendLast(Protocol.ERROR, Protocol.error(Protocol.E_DISABLED, "伙伴控制端口正在关闭"));
                return false;
            }
            if (!active.compareAndSet(null, this)) {
                Connection other = active.get();
                sendLast(Protocol.ERROR, Protocol.error(Protocol.E_BUSY, "已经有一个控制连接："
                        + (other != null ? other.displayName() : "未知") + "，请等它结束"));
                log.warn("拒绝控制连接 #{}（{}）：已经有一个控制连接", id, displayName());
                return false;
            }
            controlling = true;
            // WELCOME 在服务器主线程上生成，ready 也在同一个主线程任务里置上。事件都在主线程上发（control.event），
            // 于是每件事要么已经体现在 WELCOME 里，要么排在 WELCOME 后面发出去，中间不会漏。以前在网络线程上读每 tick 末尾
            // 才刷新的快照：握手落在一个 tick 中间时，这个 tick 里的接管、task_finished 既不在 WELCOME 里、也发不出去
            // （两半联调时实测，重启后每次都是 WELCOME 里没有伙伴、也收不到 adopted）。
            WelcomeTask w = new WelcomeTask(use);
            handler.onMainThread(w);
            boolean done;
            try {
                done = w.await(WELCOME_WAIT_MS);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new IOException("连接线程被中断");
            }
            if (!done) {
                log.warn("拒绝控制连接 #{}（{}）：服务器主线程 {} 秒没空生成 WELCOME（服务器卡住了或者正在关闭）", id, displayName(),
                        WELCOME_WAIT_MS / 1000);
                sendLast(Protocol.ERROR, Protocol.error(Protocol.E_DISABLED,
                        "服务器主线程 " + WELCOME_WAIT_MS / 1000 + " 秒没空（服务器卡住了或者正在关闭），请稍后再连"));
                controlling = false;
                active.compareAndSet(this, null);
                return false;
            }
            if (!w.queued) {
                throw new IOException("发送队列满了");
            }
            log.info("控制连接已建立：{}（#{}，来自 {}，协议版本 {}）", displayName(), id, socket.getRemoteSocketAddress(), use);
            return true;
        }

        /**
         * 在服务器主线程上生成 WELCOME、放进发送队列、置 ready（同一个主线程任务里，和事件排好先后）。网络线程最多等
         * {@link #WELCOME_WAIT_MS}；等不及了就作废，之后主线程才轮到它时什么都不做。
         */
        private final class WelcomeTask implements Runnable {
            private final int version;
            private boolean done;
            private boolean abandoned;
            /** WELCOME 放进了发送队列（ready 已置上）。 */
            boolean queued;

            WelcomeTask(int version) {
                this.version = version;
            }

            @Override
            public void run() {
                synchronized (this) {
                    if (abandoned) {
                        return;
                    }
                }
                String welcome;
                try {
                    welcome = handler.welcomeJson(version);
                } catch (RuntimeException e) {
                    log.error("生成 WELCOME 失败", e);
                    welcome = "{}";
                }
                synchronized (this) {
                    try {
                        if (!abandoned && !socket.isClosed()) {
                            queued = outbox.offer(Protocol.encode(Protocol.WELCOME, Protocol.versionAnd(version, welcome)));
                            if (queued) {
                                ready = true;
                            }
                        }
                    } catch (RuntimeException e) {
                        log.error("发不出 WELCOME", e);
                    } finally {
                        done = true;
                        notifyAll();
                    }
                }
            }

            /** 等主线程做完；超时返回 false，并作废这个任务。 */
            synchronized boolean await(long ms) throws InterruptedException {
                long deadline = System.currentTimeMillis() + ms;
                while (!done) {
                    long left = deadline - System.currentTimeMillis();
                    if (left <= 0) {
                        abandoned = true;
                        return false;
                    }
                    wait(left);
                }
                return true;
            }
        }

        private String readLoop(InputStream in) throws IOException {
            while (true) {
                Protocol.Frame f = Protocol.read(in);
                if (f == null) {
                    return endReason != null ? endReason : "对方关闭了连接";
                }
                switch (f.type) {
                    case Protocol.PING:
                        pong(f.payload);
                        break;
                    case Protocol.REQUEST:
                        handler.onRequest(f.text(), this);
                        break;
                    case Protocol.END:
                        String why = f.text().trim();
                        return "MineDeck 结束了连接" + (why.isEmpty() ? "" : "（" + why + "）");
                    default:
                        if (!warnedUnknown) {
                            warnedUnknown = true;
                            log.warn("控制连接发来了不认识的帧类型 0x{}，已忽略", Integer.toHexString(f.type));
                        }
                        break;
                }
            }
        }

        /** 放进发送队列；排不进（对方很久没读数据）就断开。可以在任意线程上调用，不会卡住。 */
        void send(int type, byte[] payload) {
            if (!ready && type != Protocol.ERROR) {
                return;
            }
            byte[] frame;
            try {
                frame = Protocol.encode(type, payload);
            } catch (IllegalArgumentException e) {
                log.warn("要发的帧太长（{} 字节），丢掉", payload.length);
                return;
            }
            if (!outbox.offer(frame)) {
                if (endReason == null) {
                    endReason = "发送队列满了（对方一直不读数据），断开";
                    log.warn("控制连接 {} 的发送队列满了（对方一直不读数据？），断开", displayName());
                }
                closeQuietly(socket);
            }
        }

        /** 主动结束这个连接（关服、关掉端口、改端口、心跳超时）：END 排进发送队列，最多等 0.5 秒让它发出去，然后断开。 */
        void end(String reason) {
            if (endReason == null) {
                endReason = reason;
            }
            sendLast(Protocol.END, Protocol.utf8(reason));
        }

        /** 发最后一帧然后断开：排进发送队列（排不进就算了），最多等 0.5 秒让它发出去。 */
        private void sendLast(int type, byte[] payload) {
            ready = false;
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
                if (endReason == null) {
                    endReason = "对方 " + WRITE_STALL_MS / 1000 + " 秒没读数据，断开";
                }
                closeQuietly(socket);
                throw new IOException(endReason);
            }
        }

        /** 发送线程是不是卡在一次写入上超过 5 秒了（对方不读数据）；是的话断开。 */
        boolean stalled() {
            long since = writingSince;
            if (since != 0 && System.nanoTime() - since > TimeUnit.MILLISECONDS.toNanos(WRITE_STALL_MS)) {
                if (endReason == null) {
                    endReason = "对方 " + WRITE_STALL_MS / 1000 + " 秒没读数据，断开";
                    log.warn("控制连接 {}：{}", displayName(), endReason);
                }
                closeQuietly(socket);
                return true;
            }
            return false;
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
            }, "MineDeck-Companion-Send-" + id);
            t.setDaemon(true);
            sender = t;
            t.start();
        }

        String displayName() {
            return client.isEmpty() ? "未署名的客户端" : "「" + client + "」";
        }
    }

    /** 发送队列里的「发完就断开」标记（按引用比较）。 */
    private static final byte[] CLOSE = new byte[0];

    static void closeQuietly(Socket s) {
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
