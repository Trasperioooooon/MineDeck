package minedeck.voicebridge;

import de.maxhenkel.voicechat.api.opus.OpusEncoder;

import java.util.ArrayDeque;
import java.util.concurrent.atomic.AtomicLong;

/**
 * 音频引擎：一个每 20 毫秒走一步的线程。这个线程只做音频（取帧、编码、交给 SVC），不碰 socket 和文件：
 * 读配置、发 STATUS 这些定时检查在 {@link VoiceBridge} 自己的线程里，对方不读数据、磁盘卡顿都拖不住音频。
 *
 * <p>每一步从抖动缓冲里取一帧（960 个采样），用 SVC 的 Opus 编码器编一次，同一份数据发给所有听众。
 * 抖动缓冲：广播开始时先攒 3 帧（60 毫秒）再播；超过 10 帧就丢掉最旧的；取空了就跳过这一步，
 * 重新攒 3 帧。发送端的时钟比服务器稍快时缓冲会慢慢变长，所以缓冲连续 1 秒都多于 5 帧就丢一帧，
 * 把延迟拉回来。连续 5 步（100 毫秒）没有声音就让 SVC 收尾（flush）并重置编码器。
 *
 * <p>编码器只在这个线程里用，线程结束时由它关闭（SVC 2.6.3 以下不关闭会泄漏内存）。
 *
 * <p>没有广播连接、又开了自测模式时，用 {@link SelfTestTone} 生成的测试音代替。
 * 定时按"开始时间 + 第几帧 × 20 毫秒"算，偶尔睡过头会在下一步追回来；落后超过 100 毫秒（比如 GC 卡顿）
 * 就重新对齐，不补发。
 */
final class AudioEngine implements Runnable {

    static final long FRAME_NANOS = 20_000_000L;
    static final int PREBUFFER_FRAMES = 3;
    static final int MAX_BUFFER_FRAMES = 10;
    static final int HIGH_WATER_FRAMES = 5;
    static final int HIGH_WATER_STEPS = 50;
    static final int IDLE_STEPS_BEFORE_FLUSH = 5;

    private final OpusEncoder encoder;
    private final Listeners listeners;

    private final Object lock = new Object();
    private final ArrayDeque<short[]> queue = new ArrayDeque<short[]>();
    private boolean buffering = true;
    private int highSteps;
    private volatile boolean sessionActive;
    private volatile boolean selftest;
    /** 只在引擎线程里用：自测模式刚打开时测试音从头放（不在别的线程里改它的进度）。 */
    private boolean selftestSeen;
    private final SelfTestTone tone = new SelfTestTone();

    private volatile boolean running;
    private Thread thread;

    // 只在引擎线程里读写
    private boolean playing;
    private int idleSteps;
    private int source = Protocol.SOURCE_NONE;
    private long encodeErrors;
    private long lastEncodeErrorLog;

    /** 本次广播连接的计数（新连接建立时清零），给 STATUS 帧用。 */
    final Counters session = new Counters();
    /** 统计日志用，每次打印后清零。 */
    final Counters window = new Counters();

    static final class Counters {
        final AtomicLong received = new AtomicLong();
        final AtomicLong played = new AtomicLong();
        final AtomicLong sent = new AtomicLong();
        final AtomicLong bytes = new AtomicLong();
        final AtomicLong dropped = new AtomicLong();
        final AtomicLong underruns = new AtomicLong();

        void reset() {
            received.set(0);
            played.set(0);
            sent.set(0);
            bytes.set(0);
            dropped.set(0);
            underruns.set(0);
        }
    }

    AudioEngine(OpusEncoder encoder, Listeners listeners) {
        this.encoder = encoder;
        this.listeners = listeners;
    }

    void start() {
        if (encoder == null) {
            Log.warn("没有 Opus 编码器，音频引擎只做定时检查，不推送声音");
        }
        running = true;
        thread = new Thread(this, "MineDeck-VoiceBridge-Audio");
        thread.setDaemon(true);
        thread.start();
    }

    void stop() {
        running = false;
        Thread t = thread;
        if (t == null && encoder != null) {
            // 线程没启动过，自己关编码器
            try {
                encoder.close();
            } catch (Throwable ignored) {
                // 关不掉也没关系
            }
        }
        if (t != null) {
            t.interrupt();
            if (t != Thread.currentThread()) {
                try {
                    t.join(2000);
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
            }
        }
    }

    void setSelftest(boolean on) {
        selftest = on;
    }

    boolean selftest() {
        return selftest;
    }

    /** 新的广播连接建立。 */
    void sessionStarted() {
        synchronized (lock) {
            queue.clear();
            buffering = true;
            highSteps = 0;
        }
        session.reset();
        sessionActive = true;
    }

    /** 广播连接断开。缓冲里剩下的帧照常播完。 */
    void sessionEnded() {
        sessionActive = false;
    }

    /** 连接线程调用：收到一帧 PCM。 */
    void offer(short[] pcm) {
        boolean dropped = false;
        synchronized (lock) {
            if (queue.size() >= MAX_BUFFER_FRAMES) {
                queue.pollFirst();
                dropped = true;
            }
            queue.addLast(pcm);
        }
        session.received.incrementAndGet();
        window.received.incrementAndGet();
        if (dropped) {
            session.dropped.incrementAndGet();
            window.dropped.incrementAndGet();
        }
    }

    int buffered() {
        synchronized (lock) {
            return queue.size();
        }
    }

    /** 现在正在推的是什么：0 没有，1 广播，2 自测音。 */
    int currentSource() {
        return playing ? source : Protocol.SOURCE_NONE;
    }

    @Override
    public void run() {
        try {
            loop();
        } finally {
            if (encoder != null) {
                try {
                    encoder.close();
                } catch (Throwable ignored) {
                    // 关不掉也没关系
                }
            }
        }
    }

    private void loop() {
        long next = System.nanoTime();
        while (running) {
            try {
                step();
            } catch (Throwable t) {
                Log.error("音频引擎出错", t);
            }
            next += FRAME_NANOS;
            long wait = next - System.nanoTime();
            if (wait < -5 * FRAME_NANOS) {
                next = System.nanoTime();
                continue;
            }
            if (wait > 0) {
                try {
                    Thread.sleep(wait / 1_000_000L, (int) (wait % 1_000_000L));
                } catch (InterruptedException e) {
                    break;
                }
            }
        }
    }

    private void step() {
        int[] from = new int[1];
        short[] pcm = nextFrame(from);
        if (pcm == null) {
            if (playing && ++idleSteps >= IDLE_STEPS_BEFORE_FLUSH) {
                playing = false;
                listeners.flush();
                if (encoder != null) {
                    encoder.resetState();
                }
                Log.info("音频暂停（" + sourceName(source) + "，100 毫秒没有新数据），已通知客户端收尾");
            }
            return;
        }
        idleSteps = 0;
        if (encoder == null) {
            return;
        }
        byte[] opus;
        try {
            opus = encoder.encode(pcm);
        } catch (Throwable t) {
            encodeError("Opus 编码失败", t);
            return;
        }
        if (opus == null || opus.length == 0) {
            encodeError("Opus 编码器返回了空数据", null);
            return;
        }
        int listenerCount = listeners.count();
        if (!playing || from[0] != source) {
            Log.info((playing ? "音源切换为" : "开始推送音频，音源：") + sourceName(from[0])
                    + "；Opus 编码正常（960 个采样 → " + opus.length + " 字节）；当前听众 " + listenerCount + " 位"
                    + (listenerCount == 0 ? "（没有听众时只编码不发送）" : ""));
        }
        playing = true;
        source = from[0];
        if (source == Protocol.SOURCE_BROADCAST) {
            session.played.incrementAndGet();
            session.bytes.addAndGet(opus.length);
        }
        window.played.incrementAndGet();
        window.bytes.addAndGet(opus.length);
        if (listenerCount > 0) {
            listeners.send(opus);
            window.sent.incrementAndGet();
            if (source == Protocol.SOURCE_BROADCAST) {
                session.sent.incrementAndGet();
            }
        }
    }

    private short[] nextFrame(int[] from) {
        synchronized (lock) {
            if (!queue.isEmpty()) {
                if (buffering && sessionActive && queue.size() < PREBUFFER_FRAMES) {
                    return null;
                }
                buffering = false;
                if (queue.size() > HIGH_WATER_FRAMES) {
                    if (++highSteps >= HIGH_WATER_STEPS) {
                        // 发送端比我们快：丢一帧，把缓冲拉回来
                        highSteps = 0;
                        queue.pollFirst();
                        session.dropped.incrementAndGet();
                        window.dropped.incrementAndGet();
                    }
                } else {
                    highSteps = 0;
                }
                from[0] = Protocol.SOURCE_BROADCAST;
                return queue.pollFirst();
            }
            if (sessionActive) {
                if (!buffering) {
                    buffering = true;
                    if (playing) {
                        session.underruns.incrementAndGet();
                        window.underruns.incrementAndGet();
                    }
                }
                return null;
            }
        }
        boolean on = selftest;
        if (on && !selftestSeen) {
            tone.reset();
        }
        selftestSeen = on;
        if (on) {
            from[0] = Protocol.SOURCE_SELFTEST;
            return tone.next();
        }
        return null;
    }

    private void encodeError(String message, Throwable t) {
        encodeErrors++;
        long now = System.currentTimeMillis();
        if (now - lastEncodeErrorLog > 30_000L) {
            lastEncodeErrorLog = now;
            Log.error(message + "（累计 " + encodeErrors + " 次）", t);
        }
        if (encoder != null) {
            try {
                encoder.resetState();
            } catch (Throwable ignored) {
                // 下一帧再试
            }
        }
    }

    static String sourceName(int source) {
        switch (source) {
            case Protocol.SOURCE_BROADCAST:
                return "广播";
            case Protocol.SOURCE_SELFTEST:
                return "自测音";
            default:
                return "无";
        }
    }
}
