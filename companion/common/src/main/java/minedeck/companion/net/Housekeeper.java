package minedeck.companion.net;

import java.io.IOException;
import java.nio.file.Path;
import java.util.Arrays;

import org.slf4j.Logger;

/**
 * 定时检查线程「MineDeck-Companion-Housekeeping」：每 2 秒重读一次配置文件、端口被占时重试、给控制连接发 STATUS。
 *
 * <p>配置按内容判断变没变：读出文件的全部字节，和上次应用的逐字节比较；发现变了，隔 0.15 秒再读一遍，两次一样才应用
 * （别的程序可能正在一点点写这个文件），不一样就等下一轮。不看修改时间和大小：同一毫秒里连写两次、或者修改时间只精确到
 * 2 秒的盘（FAT32），按修改时间判断会漏掉改动。
 *
 * <p>关服时先 {@link #stop()} 这个线程（等它真的退出），再关端口：免得它刚读完配置、端口关掉之后又去应用，
 * 留下一个没人管的监听端口（{@link ControlServer} 关掉之后也不会再开端口，双保险）。
 */
public final class Housekeeper {

    /** 配置应用到游戏里（伙伴上限、禁挖区域……）。定时检查线程上调用，实现要线程安全。 */
    public interface Listener {
        void onConfig(CompanionConfig config, boolean first);
    }

    public static final long INTERVAL_MS = 2000L;
    public static final long SETTLE_MS = 150L;

    private final Path file;
    private final ControlServer control;
    private final Listener listener;
    private final Logger log;
    private byte[] applied;
    private boolean warnedMissing;
    private String lastReadError;
    private volatile boolean stopped;
    private Thread thread;

    public Housekeeper(Path file, ControlServer control, Listener listener, Logger log) {
        this.file = file;
        this.control = control;
        this.listener = listener;
        this.log = log;
    }

    /** 开服时在主线程上调用：读一次配置（没有就生成模板）并应用，然后启动定时检查线程。 */
    public synchronized void start() {
        CompanionConfig cfg = load(true);
        if (cfg == null) {
            cfg = CompanionConfig.disabled();
        }
        apply(cfg, true);
        Thread t = new Thread(this::loop, "MineDeck-Companion-Housekeeping");
        t.setDaemon(true);
        thread = t;
        t.start();
    }

    /** 停下定时检查线程，最多等 2 秒让它退出。 */
    public void stop() {
        stopped = true;
        Thread t;
        synchronized (this) {
            t = thread;
        }
        if (t != null && t != Thread.currentThread()) {
            t.interrupt();
            try {
                t.join(2000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
        }
    }

    private void loop() {
        while (!stopped) {
            try {
                Thread.sleep(INTERVAL_MS);
            } catch (InterruptedException e) {
                return;
            }
            if (stopped) {
                return;
            }
            try {
                CompanionConfig cfg = load(false);
                if (cfg != null && !stopped) {
                    apply(cfg, false);
                }
                if (!stopped) {
                    control.tick();
                }
            } catch (Throwable t) {
                log.error("伙伴模组的定时检查出错", t);
            }
        }
    }

    private void apply(CompanionConfig cfg, boolean first) {
        if (first) {
            log.info("伙伴配置：{}（{}）", cfg.describe(), file);
        } else {
            log.info("伙伴配置文件已重新读取：{}", cfg.describe());
        }
        for (String p : cfg.problems) {
            log.warn("伙伴配置没生效的条目：{}", p);
        }
        control.apply(cfg);
        listener.onConfig(cfg, first);
    }

    /** 读配置；内容没变（或者还在写、读不了）时返回 null。first 为真时文件不存在就生成模板。 */
    private CompanionConfig load(boolean first) {
        byte[] bytes;
        try {
            bytes = CompanionConfig.read(file);
        } catch (IOException e) {
            readError(e);
            return null;
        }
        if (bytes == null) {
            if (first) {
                try {
                    CompanionConfig.writeTemplate(file);
                    log.info("没找到伙伴配置文件，已生成模板：{}（控制端口默认关闭；从 MineDeck 开服时会自动写好）", file);
                    bytes = CompanionConfig.read(file);
                } catch (IOException e) {
                    log.error("生成伙伴配置文件失败：" + file, e);
                }
            } else if (!warnedMissing) {
                warnedMissing = true;
                log.warn("伙伴配置文件不见了：{}，继续用之前的设置", file);
            }
            if (bytes == null) {
                return null;
            }
        }
        warnedMissing = false;
        lastReadError = null;
        if (applied != null && Arrays.equals(bytes, applied)) {
            return null;
        }
        if (!first) {
            try {
                Thread.sleep(SETTLE_MS);
                byte[] again = CompanionConfig.read(file);
                if (again == null || !Arrays.equals(again, bytes)) {
                    return null; // 还在写，下一轮再看
                }
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return null;
            } catch (IOException e) {
                readError(e);
                return null;
            }
        }
        applied = bytes;
        try {
            return CompanionConfig.parse(bytes);
        } catch (IOException | RuntimeException e) {
            log.error("读取伙伴配置文件失败：" + file + "，继续用之前的设置", e);
            return null;
        }
    }

    /** 读不了配置文件（比如被别的程序独占着）：同样的错误只记一次；关服时被打断的不记。 */
    private void readError(IOException e) {
        if (stopped) {
            return;
        }
        String msg = String.valueOf(e.getMessage());
        if (!msg.equals(lastReadError)) {
            lastReadError = msg;
            log.error("读取伙伴配置文件失败：" + file + "，继续用之前的设置", e);
        }
    }
}
