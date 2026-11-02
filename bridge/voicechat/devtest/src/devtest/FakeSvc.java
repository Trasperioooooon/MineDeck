package devtest;

import com.mojang.authlib.GameProfile;
import de.maxhenkel.voicechat.api.ServerLevel;
import de.maxhenkel.voicechat.api.ServerPlayer;
import de.maxhenkel.voicechat.api.VoicechatConnection;
import de.maxhenkel.voicechat.api.VoicechatPlugin;
import de.maxhenkel.voicechat.api.VoicechatServerApi;
import de.maxhenkel.voicechat.api.VolumeCategory;
import de.maxhenkel.voicechat.api.audiochannel.StaticAudioChannel;
import de.maxhenkel.voicechat.api.events.EventRegistration;
import de.maxhenkel.voicechat.api.events.PlayerConnectedEvent;
import de.maxhenkel.voicechat.api.events.PlayerDisconnectedEvent;
import de.maxhenkel.voicechat.api.events.VoicechatServerStartedEvent;
import de.maxhenkel.voicechat.api.events.VoicechatServerStoppedEvent;
import de.maxhenkel.voicechat.api.opus.OpusEncoder;

import java.io.BufferedReader;
import java.io.DataInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.StandardCopyOption;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Properties;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.BooleanSupplier;
import java.util.function.Consumer;
import java.util.regex.Pattern;
import java.util.zip.ZipEntry;
import java.util.zip.ZipFile;

/**
 * devtest：不装 Minecraft，用一个假的 SVC 服务端加载桥接模组。
 *
 * <p>找插件的方式和 SVC 在 Forge / NeoForge 上一样：加载器扫描 class 文件里的注解（这里自己解析 class
 * 文件，和 FML 用 ASM 扫描是一回事），SVC 再 {@code Class.forName} + 无参构造函数创建插件。之后模拟语音
 * 服务器启动、玩家连上和断开，用 testsend 发送音频，逐项检查结果。假 SVC 只用动态代理实现运行时
 * 那一版 API 里真有的接口，所以可以拿新旧两版 API 各跑一遍。
 *
 * <p>用法：FakeSvc --jar &lt;模组 jar&gt; --testsend &lt;testsend 可执行文件&gt; --work &lt;临时目录&gt;
 */
public final class FakeSvc {

    private static final String PLUGIN_ANNOTATION = "Lde/maxhenkel/voicechat/api/ForgeVoicechatPlugin;";
    private static int failures;
    private static int passes;

    public static void main(String[] args) throws Exception {
        Map<String, String> a = new HashMap<>();
        for (int i = 0; i + 1 < args.length; i += 2) {
            a.put(args[i].replaceFirst("^--", ""), args[i + 1]);
        }
        Path jar = Paths.get(a.get("jar"));
        String testsend = a.get("testsend");
        Path work = Paths.get(a.get("work"));
        Files.createDirectories(work);
        Path config = work.resolve("config/minedeck/voicebridge.properties");
        Files.deleteIfExists(config);
        System.setProperty("minedeck.voicebridge.config", config.toString());

        boolean modern = hasMethod(VoicechatServerApi.class, "createStaticAudioChannel", UUID.class)
                && hasMethod(StaticAudioChannel.class, "addTarget", VoicechatConnection.class);
        System.out.println("运行时的 SVC API：" + (modern ? "有" : "没有") + " createStaticAudioChannel(UUID) 和 addTarget"
                + "，模组应该用" + (modern ? "多目标" : "旧版兼容（每位听众一个通道）") + "方式");

        // ---------------------------------------------------------------------------------
        section("1. 按 SVC 在 Forge / NeoForge 上的方式找插件");
        List<String> found = scanForPlugins(jar);
        check(found.equals(List.of("minedeck.voicebridge.BridgePlugin")), "扫描 class 文件，找到带 @ForgeVoicechatPlugin 的类：" + found);
        Class<?> cls = Class.forName(found.get(0));
        check(VoicechatPlugin.class.isAssignableFrom(cls), "这个类实现了 VoicechatPlugin");
        VoicechatPlugin plugin = (VoicechatPlugin) cls.getDeclaredConstructor().newInstance();
        check("minedeck_voicebridge".equals(plugin.getPluginId()), "用无参构造函数创建成功，插件 id = " + plugin.getPluginId());

        FakeServer server = new FakeServer(modern);
        plugin.initialize(server.api);
        Events events = new Events();
        plugin.registerEvents(events.registration());
        check(events.handlers.keySet().containsAll(List.of(VoicechatServerStartedEvent.class, VoicechatServerStoppedEvent.class,
                PlayerConnectedEvent.class, PlayerDisconnectedEvent.class)), "注册了事件：" + events.names());

        // ---------------------------------------------------------------------------------
        section("2. 语音服务器启动（这时还没有配置文件）");
        events.fire(VoicechatServerStartedEvent.class, server.event(VoicechatServerStartedEvent.class, null, null));
        check(Files.exists(config), "自动生成了配置模板：" + config);
        Properties tpl = loadProperties(config);
        check("false".equals(tpl.getProperty("enabled")), "模板里 enabled=false（不是从 MineDeck 开服时不开端口）");
        check(tpl.getProperty("token", "").matches("[0-9a-f]{32}"), "模板里有随机生成的 32 位令牌");
        check(server.categories.size() == 1, "注册了 1 个音量分类");
        if (!server.categories.isEmpty()) {
            Map<String, Object> c = server.categories.get(0);
            check("minedeck_music".equals(c.get("id")), "分类 id = " + c.get("id"));
            check("MineDeck 音乐共享".equals(c.get("name")), "分类名 = " + c.get("name") + "（" + ((String) c.get("name")).length() + " 个字符，SVC 限 16 个）");
            check(c.get("description") != null, "分类说明 = " + c.get("description"));
            int[][] icon = (int[][]) c.get("icon");
            check(icon != null && icon.length == 16 && icon[0].length == 16, "分类图标是 16×16");
        }
        check(server.encoders.size() == 1 && "AUDIO".equals(String.valueOf(server.encoders.get(0).mode)), "用 AUDIO 模式创建了 Opus 编码器");
        if (modern) {
            check(server.channels.size() == 1, "建了 1 个共用的静态音频通道");
            if (!server.channels.isEmpty()) {
                FakeChannel ch = server.channels.get(0);
                check("minedeck_music".equals(ch.category), "通道归到 minedeck_music 分类");
                check(Boolean.TRUE.equals(ch.bypass) || !hasMethod(StaticAudioChannel.class, "setBypassGroupIsolation", boolean.class),
                        "通道设置了 bypassGroupIsolation=" + ch.bypass);
            }
        }

        // ---------------------------------------------------------------------------------
        section("3. 写入配置：打开端口，排除 Host（模组每 2 秒检查一次配置文件）；端口先被别的程序占着");
        int port = freePort();
        String token = "devtest" + UUID.randomUUID().toString().replace("-", "");
        String addr = "127.0.0.1:" + port;
        ServerSocket squatter = new ServerSocket();
        squatter.bind(new InetSocketAddress("127.0.0.1", port));
        writeConfig(config, true, port, token, "Host", false);
        Thread.sleep(3000);
        squatter.close();
        check(!portOpen(port), "端口被占用时模组打不开（日志里有提示），占用的程序退出后端口暂时空着");
        check(waitUntil(13000, () -> portOpen(port)), "模组每 10 秒重试一次，端口 " + addr + " 已打开");

        // ---------------------------------------------------------------------------------
        section("4. 三位玩家连上语音");
        FakePlayer alice = server.player("Alice");
        FakePlayer bob = server.player("Bob");
        FakePlayer host = server.player("Host");
        for (FakePlayer p : List.of(alice, bob, host)) {
            server.connect(p);
            events.fire(PlayerConnectedEvent.class, server.event(PlayerConnectedEvent.class, p.connection, null));
        }
        if (modern) {
            check(server.channels.get(0).targets.equals(Set.of(alice.id, bob.id)),
                    "共用通道的听众是 Alice 和 Bob（Host 按名字排除，名字是用反射从假 ServerPlayer 的 m_36316_() 取到的）");
        } else {
            check(server.channelTargets().equals(Set.of(alice.id, bob.id)), "给 Alice 和 Bob 各建了一个通道，Host 被排除");
        }

        // ---------------------------------------------------------------------------------
        section("5. 令牌不对");
        Result bad = run(testsend, "-addr", addr, "-token", "wrong-token-0000000000", "-duration", "1s", "-quiet");
        check(bad.code == 2 && bad.out.contains("错误码 1"), "testsend 被拒绝（退出码 " + bad.code + "，ERROR 1）");

        // ---------------------------------------------------------------------------------
        section("6. 正常广播 3 秒（440 Hz）；期间第二个发送端来连，应被拒绝");
        server.resetCounters();
        Proc first = start(testsend, "-addr", addr, "-token", token, "-duration", "3s", "-name", "sender-1");
        check(waitUntil(3000, () -> server.received(alice) > 0), "Alice 开始收到音频");
        Result second = run(testsend, "-addr", addr, "-token", token, "-duration", "1s", "-name", "sender-2", "-quiet");
        check(second.code == 2 && second.out.contains("错误码 3"), "第二个连接被拒绝（退出码 " + second.code + "，ERROR 3：已经有一个广播连接）");
        Result r1 = first.waitFor(15000);
        check(r1.code == 0 && r1.out.contains("握手成功") && r1.out.contains("发送完毕"), "第一个发送端正常结束（退出码 " + r1.code + "）");
        check(r1.out.contains("状态："), "发送端收到了模组每 2 秒发的 STATUS");
        check(waitUntil(2000, () -> server.flushes.get() > 0), "广播结束 100 毫秒后通知客户端收尾（flush）");
        int ra = server.received(alice);
        int rb = server.received(bob);
        int rh = server.received(host);
        check(ra >= 145 && ra <= 150, "Alice 收到 " + ra + " 帧（3 秒应为 150 帧）");
        check(rb == ra, "Bob 收到 " + rb + " 帧，和 Alice 一样");
        check(rh == 0, "Host 被排除，收到 " + rh + " 帧");
        FakeEncoder enc = server.encoders.get(0);
        check(enc.encoded.get() >= ra && enc.wrongSize.get() == 0, "编码器编了 " + enc.encoded.get() + " 帧，每帧都是 960 个采样");
        check(enc.loud.get() >= ra - 2, "编码器收到的不是静音（" + enc.loud.get() + " 帧有声音）");
        check(server.sendCallsTotal() == (modern ? ra : 2L * ra), "每帧只编码一次，同一份数据发给所有听众（send 调用 " + server.sendCallsTotal() + " 次）");

        // ---------------------------------------------------------------------------------
        section("7. 发送端被强行结束（模拟 MineDeck 崩溃），然后重新连接");
        Proc killed = start(testsend, "-addr", addr, "-token", token, "-duration", "0", "-quiet", "-name", "killed");
        check(waitUntil(3000, () -> killed.output().contains("握手成功")), "连上了");
        Thread.sleep(800);
        killed.kill();
        Thread.sleep(300);
        Result again = run(testsend, "-addr", addr, "-token", token, "-duration", "1s", "-quiet", "-name", "reconnect");
        check(again.code == 0 && again.out.contains("握手成功"), "强行断开后立刻重连成功（退出码 " + again.code + "）");

        // ---------------------------------------------------------------------------------
        section("8. 心跳超时：握手后 7 秒什么都不发，模组应在 5 秒左右断开");
        long t0 = System.nanoTime();
        Result stalled = run(testsend, "-addr", addr, "-token", token, "-stall", "7s", "-duration", "1s", "-quiet");
        long ms = (System.nanoTime() - t0) / 1_000_000L;
        check(stalled.code == 3 && ms >= 4500 && ms < 6900, "模组在 " + ms + " 毫秒后断开了连接（退出码 " + stalled.code + "）");

        // ---------------------------------------------------------------------------------
        section("9. 协议错误");
        check(rawHello(port, token, 2) == 2, "协议版本 2 → ERROR 2");
        check(rawBadPcm(port, token) == 4, "PCM 帧长度不对 → ERROR 4");
        check(rawFirstFrameNotHello(port) == 4, "第一帧不是 HELLO → ERROR 4");
        check(rawHello(port, token, 1) == 0, "之后正常握手仍然成功（这次用中文名字「房主」，见上面模组的日志）");

        // ---------------------------------------------------------------------------------
        section("10. Bob 断开语音");
        server.disconnect(bob);
        events.fire(PlayerDisconnectedEvent.class, server.event(PlayerDisconnectedEvent.class, null, bob.id));
        server.resetCounters();
        Result afterBob = run(testsend, "-addr", addr, "-token", token, "-duration", "1s", "-quiet");
        check(afterBob.code == 0, "广播 1 秒");
        check(server.received(alice) >= 45 && server.sendCallsTo(bob) == 0,
                "Alice 收到 " + server.received(alice) + " 帧；模组不再给 Bob 发（" + server.sendCallsTo(bob) + " 次）");

        // ---------------------------------------------------------------------------------
        section("11. 改配置：清空排除名单，打开自测模式（不用重启）");
        server.resetCounters();
        writeConfig(config, true, port, token, "", true);
        check(waitUntil(5000, () -> server.received(host) >= 25 && server.received(alice) >= 25),
                "没有广播连接时，Alice 和 Host 都收到了测试音（Alice " + server.received(alice) + " 帧，Host " + server.received(host) + " 帧）");
        check(server.encoders.get(0).loud.get() > 0, "测试音不是静音");
        Result during = run(testsend, "-addr", addr, "-token", token, "-duration", "3s", "-name", "over-selftest");
        check(during.code == 0 && during.out.contains("音源 广播"), "自测模式开着时来了广播：STATUS 显示音源切换为广播");
        int resumedFrom = server.received(alice);
        check(waitUntil(3000, () -> server.received(alice) >= resumedFrom + 25), "广播结束后自测音自动接着放");
        writeConfig(config, true, port, token, "", false);
        check(waitUntil(5000, () -> {
            int before = server.received(alice);
            sleepQuietly(300);
            return server.received(alice) == before;
        }), "关闭自测模式后停止推送");

        // ---------------------------------------------------------------------------------
        section("12. 发送端比实时快 2%（模拟两边时钟有偏差），缓冲不应一直变长");
        Result fast = run(testsend, "-addr", addr, "-token", token, "-duration", "12s", "-speed", "1.02", "-name", "fast-clock");
        String lastStatus = "";
        for (String line : fast.out.split("\n")) {
            if (line.startsWith("状态：")) {
                lastStatus = line;
            }
        }
        java.util.regex.Matcher bm = Pattern.compile("缓冲 (\\d+) 帧.*丢弃 (\\d+)").matcher(lastStatus);
        boolean parsed = bm.find();
        int buffered = parsed ? Integer.parseInt(bm.group(1)) : -1;
        int dropped = parsed ? Integer.parseInt(bm.group(2)) : -1;
        check(fast.code == 0 && parsed && buffered <= 7 && dropped >= 2,
                "最后一次 STATUS：缓冲 " + buffered + " 帧（不超过 7 帧，约 140 毫秒），为追平时钟丢了 " + dropped + " 帧");

        // ---------------------------------------------------------------------------------
        section("13. 改配置：enabled=false 关闭端口");
        writeConfig(config, false, port, token, "", false);
        check(waitUntil(5000, () -> !portOpen(port)), "端口已关闭");
        writeConfig(config, true, port, token, "", false);
        check(waitUntil(5000, () -> portOpen(port)), "改回 enabled=true 后端口重新打开");

        // ---------------------------------------------------------------------------------
        section("13a. 配置文件改了，但修改时间和大小都没变（比如同一毫秒里连写两次）：模组也要重新读");
        writeConfig(config, true, port, token, "Host", false);
        check(waitUntil(5000, () -> listeners(plugin).equals(Set.of(alice.id))), "排除 Host 后听众只剩 Alice：" + listeners(plugin));
        java.nio.file.attribute.FileTime stampBefore = Files.getLastModifiedTime(config);
        long sizeBefore = Files.size(config);
        writeConfig(config, true, port, token, "Abcd", false); // 和 Host 一样长
        Files.setLastModifiedTime(config, stampBefore);
        check(Files.size(config) == sizeBefore && Files.getLastModifiedTime(config).equals(stampBefore), "新文件的大小和修改时间都和旧的一样");
        check(waitUntil(5000, () -> listeners(plugin).equals(Set.of(alice.id, host.id))), "模组读到了新内容，Host 回到听众里：" + listeners(plugin));

        // ---------------------------------------------------------------------------------
        section("13b. 发送端只发不收（从不读模组的回复）：模组的其他线程不能被它拖住，还要主动断开它");
        writeConfig(config, true, port, token, "Alice", false);
        check(waitUntil(5000, () -> listeners(plugin).equals(Set.of(host.id))), "先排除 Alice，听众只剩 Host：" + listeners(plugin));
        Flood flood = new Flood(port, token);
        check(flood.handshakeOk, "握手成功，开始只发不收（每帧 4 KB 的 PING，模组要回同样大的 PONG）");
        Thread.sleep(1500);
        writeConfig(config, true, port, token, "Host", false);
        check(waitUntil(5000, () -> listeners(plugin).equals(Set.of(alice.id))), "只发不收开始后改配置，生效（听众只剩 Alice）：" + listeners(plugin));
        Thread.sleep(3000); // 这时双方的 socket 缓冲早就满了：模组回 PONG 的写入卡住，之后发 STATUS 也会卡住
        writeConfig(config, true, port, token, "", false);
        check(waitUntil(5000, () -> listeners(plugin).equals(Set.of(alice.id, host.id))),
                "卡住几秒之后再改配置，照样生效（Alice 和 Host 都是听众；定时检查没有被卡住）：" + listeners(plugin));
        check(flood.waitEnded(10000), "模组发现对方不读数据，断开了这个连接（" + flood.describe() + "）");
        flood.close();
        Result afterFlood = run(testsend, "-addr", addr, "-token", token, "-duration", "1s", "-quiet", "-name", "after-flood");
        check(afterFlood.code == 0 && afterFlood.out.contains("握手成功"), "之后新的广播连接正常（退出码 " + afterFlood.code + "）");

        // ---------------------------------------------------------------------------------
        section("14. 语音服务器停止");
        Proc live = start(testsend, "-addr", addr, "-token", token, "-duration", "0", "-quiet", "-name", "live");
        check(waitUntil(3000, () -> live.output().contains("握手成功")), "停服前有一个正在广播的连接");
        Object bridgeBefore = field(plugin, "current");
        server.closed = true;
        events.fire(VoicechatServerStoppedEvent.class, server.event(VoicechatServerStoppedEvent.class, null, null));
        Result liveResult = live.waitFor(5000);
        check(liveResult.code == 3 && liveResult.out.contains("语音服务器已停止"), "正在广播的连接收到 END（语音服务器已停止），退出码 " + liveResult.code);
        check(waitUntil(3000, () -> !portOpen(port)), "端口已关闭");
        check(waitUntil(3000, () -> !threadAlive("MineDeck-VoiceBridge-Audio")), "音频线程已退出");
        check(server.encoders.get(0).closed, "编码器已关闭（SVC 2.6.3 以下不关会泄漏内存）");
        check(waitUntil(3000, () -> !threadAlive("MineDeck-VoiceBridge-Housekeeping")), "定时检查线程已退出");
        // 定时检查刚读完配置、还没应用时语音服务器停了：停服之后才应用的这份配置不能把端口重新打开
        Path late = work.resolve("late.properties");
        writeConfig(late, true, port, token, "", false);
        Object lateConfig = invokeStatic("minedeck.voicebridge.BridgeConfig", "load", new Class<?>[]{Path.class}, late);
        invoke(field(bridgeBefore, "control"), "apply", new Class<?>[]{lateConfig.getClass()}, lateConfig);
        check(!portOpen(port) && waitUntil(2500, () -> !portOpen(port)) && !threadAlive("MineDeck-VoiceBridge-Accept"),
                "停服之后才应用的配置没有把端口重新打开");

        // ---------------------------------------------------------------------------------
        section("15. 语音服务器再次启动（SVC 重启语音服务器时会这样）");
        FakeServer server2 = new FakeServer(modern);
        events.fire(VoicechatServerStartedEvent.class, server2.event(VoicechatServerStartedEvent.class, null, null));
        check(waitUntil(3000, () -> portOpen(port)), "端口重新打开");
        check(server2.categories.size() == 1, "音量分类在新的语音服务器上重新注册");
        FakePlayer carol = server2.player("Carol");
        server2.connect(carol);
        events.fire(PlayerConnectedEvent.class, server2.event(PlayerConnectedEvent.class, carol.connection, null));
        Result r3 = run(testsend, "-addr", addr, "-token", token, "-duration", "1s", "-quiet");
        check(r3.code == 0 && server2.received(carol) >= 45, "重启后照常推送（Carol 收到 " + server2.received(carol) + " 帧）");

        // ---------------------------------------------------------------------------------
        section("16. MineDeck 自己的客户端（internal/bridge 的 bridge.Client）：推音频；配置里的端口和令牌改了，客户端重读配置接着连");
        server2.resetCounters();
        Proc cl = start(testsend, "-config", config.toString(), "-client", "-duration", "0", "-name", "minedeck-client");
        check(waitUntil(6000, () -> cl.output().contains("已连上桥接模组")), "客户端按配置文件连上了");
        check(waitUntil(4000, () -> server2.received(carol) >= 50), "Carol 收到了客户端推的音频（" + server2.received(carol) + " 帧）");
        int port2 = freePort();
        String token2 = "devtest" + UUID.randomUUID().toString().replace("-", "");
        writeConfig(config, true, port2, token2, "", false);
        check(waitUntil(6000, () -> portOpen(port2)), "模组改到了新端口 " + port2 + "（旧连接收到 END：端口改了）");
        check(waitUntil(10000, () -> count(cl.output(), "已连上桥接模组") >= 2 && cl.output().contains("127.0.0.1:" + port2)),
                "客户端重读了配置，用新端口和新令牌重新连上");
        int beforeSwitch = server2.received(carol);
        check(waitUntil(4000, () -> server2.received(carol) >= beforeSwitch + 50), "换端口以后 Carol 继续收到音频");
        cl.term();
        Result clResult = cl.waitFor(5000);
        check(clResult.code == 0 && clResult.out.contains("已停止"), "客户端停止时发 END 后退出（退出码 " + clResult.code + "）");
        server2.closed = true;
        events.fire(VoicechatServerStoppedEvent.class, server2.event(VoicechatServerStoppedEvent.class, null, null));

        check(server.unexpected.isEmpty() && server2.unexpected.isEmpty(),
                "模组没有调用假 SVC 没模拟的 API：" + server.unexpected + server2.unexpected);

        System.out.println();
        System.out.println("devtest 结果：通过 " + passes + " 项，失败 " + failures + " 项");
        System.exit(failures == 0 ? 0 : 1);
    }

    // ===================================================================================
    // 假 SVC

    static final class FakePlayer {
        final UUID id;
        final String name;
        VoicechatConnection connection;

        FakePlayer(String name) {
            this.name = name;
            this.id = UUID.nameUUIDFromBytes(("OfflinePlayer:" + name).getBytes(StandardCharsets.UTF_8));
        }
    }

    static final class FakeEncoder {
        final Object mode;
        final AtomicInteger encoded = new AtomicInteger();
        final AtomicInteger wrongSize = new AtomicInteger();
        final AtomicInteger loud = new AtomicInteger();
        final AtomicInteger resets = new AtomicInteger();
        volatile boolean closed;

        FakeEncoder(Object mode) {
            this.mode = mode;
        }

        byte[] encode(short[] pcm) {
            if (pcm.length != 960) {
                wrongSize.incrementAndGet();
                throw new IllegalArgumentException("帧长度不是 960：" + pcm.length);
            }
            double sum = 0;
            for (short s : pcm) {
                sum += (double) s * s;
            }
            if (Math.sqrt(sum / pcm.length) > 300) {
                loud.incrementAndGet();
            }
            encoded.incrementAndGet();
            return new byte[]{(byte) 0xFC, 1, 2, 3, 4, 5, 6, 7};
        }
    }

    static final class FakeChannel {
        final UUID id;
        final UUID legacyTarget;
        final Set<UUID> targets = ConcurrentHashMap.newKeySet();
        final AtomicInteger sendCalls = new AtomicInteger();
        volatile String category;
        volatile Boolean bypass;

        FakeChannel(UUID id, UUID legacyTarget) {
            this.id = id;
            this.legacyTarget = legacyTarget;
        }
    }

    static final class FakeServer {
        final boolean modern;
        final VoicechatServerApi api;
        final List<Map<String, Object>> categories = new CopyOnWriteArrayList<>();
        final List<FakeEncoder> encoders = new CopyOnWriteArrayList<>();
        final List<FakeChannel> channels = new CopyOnWriteArrayList<>();
        final Set<UUID> connected = ConcurrentHashMap.newKeySet();
        final Map<UUID, VoicechatConnection> connections = new ConcurrentHashMap<>();
        final Map<UUID, AtomicInteger> received = new ConcurrentHashMap<>();
        final AtomicInteger flushes = new AtomicInteger();
        final List<String> unexpected = new CopyOnWriteArrayList<>();
        volatile boolean closed;

        FakeServer(boolean modern) {
            this.modern = modern;
            this.api = proxy(VoicechatServerApi.class, (p, m, args) -> handleApi(m, args));
        }

        FakePlayer player(String name) {
            FakePlayer fp = new FakePlayer(name);
            FakeMcPlayer mc = new FakeMcPlayer(new GameProfile(fp.id, name));
            ServerLevel level = proxy(ServerLevel.class, (p, m, args) -> m.getName().equals("getServerLevel") ? new Object() : unsupported(m));
            ServerPlayer sp = proxy(ServerPlayer.class, (p, m, args) -> {
                switch (m.getName()) {
                    case "getUuid":
                        return fp.id;
                    case "getPlayer":
                    case "getEntity":
                        return mc;
                    case "getServerLevel":
                        return level;
                    default:
                        return unsupported(m);
                }
            });
            fp.connection = proxy(VoicechatConnection.class, (p, m, args) -> {
                switch (m.getName()) {
                    case "getPlayer":
                        return sp;
                    case "isConnected":
                        return connected.contains(fp.id);
                    case "isDisabled":
                    case "isInGroup":
                        return false;
                    case "isInstalled":
                        return true;
                    case "getGroup":
                        return null;
                    default:
                        return unsupported(m);
                }
            });
            return fp;
        }

        void connect(FakePlayer p) {
            connected.add(p.id);
            connections.put(p.id, p.connection);
        }

        void disconnect(FakePlayer p) {
            connected.remove(p.id);
            connections.remove(p.id);
        }

        int received(FakePlayer p) {
            AtomicInteger n = received.get(p.id);
            return n == null ? 0 : n.get();
        }

        long sendCallsTotal() {
            long n = 0;
            for (FakeChannel c : channels) {
                n += c.sendCalls.get();
            }
            return n;
        }

        long sendCallsTo(FakePlayer p) {
            long n = 0;
            for (FakeChannel c : channels) {
                if (p.id.equals(c.legacyTarget) || (c.legacyTarget == null && c.targets.contains(p.id))) {
                    n += c.sendCalls.get();
                }
            }
            return n;
        }

        Set<UUID> channelTargets() {
            Set<UUID> s = new HashSet<>();
            for (FakeChannel c : channels) {
                if (c.legacyTarget != null) {
                    s.add(c.legacyTarget);
                } else {
                    s.addAll(c.targets);
                }
            }
            return s;
        }

        void resetCounters() {
            received.clear();
            flushes.set(0);
            for (FakeChannel c : channels) {
                c.sendCalls.set(0);
            }
            for (FakeEncoder e : encoders) {
                e.encoded.set(0);
                e.loud.set(0);
            }
        }

        <T> T event(Class<T> type, VoicechatConnection connection, UUID uuid) {
            return proxy(type, (p, m, args) -> {
                switch (m.getName()) {
                    case "getVoicechat":
                        return api;
                    case "getConnection":
                        return connection;
                    case "getPlayerUuid":
                        return uuid;
                    case "isCancellable":
                    case "cancel":
                    case "isCancelled":
                        return false;
                    default:
                        return unsupported(m);
                }
            });
        }

        private Object handleApi(Method m, Object[] args) {
            String n = m.getName();
            int c = m.getParameterCount();
            switch (n) {
                case "createEncoder": {
                    FakeEncoder e = new FakeEncoder(c == 1 ? args[0] : "VOIP（默认）");
                    encoders.add(e);
                    return proxy(OpusEncoder.class, (p, mm, aa) -> {
                        switch (mm.getName()) {
                            case "encode":
                                return e.encode((short[]) aa[0]);
                            case "resetState":
                                e.resets.incrementAndGet();
                                return null;
                            case "close":
                                e.closed = true;
                                return null;
                            case "isClosed":
                                return e.closed;
                            default:
                                return unsupported(mm);
                        }
                    });
                }
                case "createStaticAudioChannel":
                    if (closed) {
                        return null;
                    }
                    if (c == 1) {
                        return channel(new FakeChannel((UUID) args[0], null));
                    }
                    if (c == 3) {
                        VoicechatConnection conn = (VoicechatConnection) args[2];
                        return channel(new FakeChannel((UUID) args[0], conn.getPlayer().getUuid()));
                    }
                    break;
                case "volumeCategoryBuilder":
                    return categoryBuilder();
                case "registerVolumeCategory": {
                    Object category = args[0];
                    Map<String, Object> f = ((CategoryHandle) Proxy.getInvocationHandler(category)).fields;
                    String name = (String) f.get("name");
                    if (name == null || name.length() > 16) {
                        // SVC 同步分类给客户端时用 writeUtf(name, 16)，超长会出错
                        unexpected.add("分类名超过 16 个字符：" + name);
                    }
                    categories.add(f);
                    return null;
                }
                case "getConnectionOf":
                    if (c == 1 && args[0] instanceof UUID) {
                        return connections.get(args[0]);
                    }
                    break;
                case "getVoiceChatDistance":
                case "getBroadcastRange":
                    return 48.0;
                default:
                    break;
            }
            return unsupported(m);
        }

        private StaticAudioChannel channel(FakeChannel ch) {
            channels.add(ch);
            return proxy(StaticAudioChannel.class, (p, m, args) -> {
                switch (m.getName()) {
                    case "send":
                        if (args[0] instanceof byte[]) {
                            ch.sendCalls.incrementAndGet();
                            deliver(ch);
                            return null;
                        }
                        return unsupported(m);
                    case "flush":
                        flushes.incrementAndGet();
                        return null;
                    case "isClosed":
                        return closed;
                    case "getId":
                        return ch.id;
                    case "getCategory":
                        return ch.category;
                    case "setCategory":
                        ch.category = (String) args[0];
                        return null;
                    case "setFilter":
                        return null;
                    case "addTarget":
                        ch.targets.add(((VoicechatConnection) args[0]).getPlayer().getUuid());
                        return null;
                    case "removeTarget":
                        ch.targets.remove(((VoicechatConnection) args[0]).getPlayer().getUuid());
                        return null;
                    case "clearTargets":
                        ch.targets.clear();
                        return null;
                    case "setBypassGroupIsolation":
                        ch.bypass = (Boolean) args[0];
                        return null;
                    case "bypassesGroupIsolation":
                        return Boolean.TRUE.equals(ch.bypass);
                    default:
                        return unsupported(m);
                }
            });
        }

        /** 和 SVC 一样：只发给还连着语音的人。 */
        private void deliver(FakeChannel ch) {
            if (ch.legacyTarget != null) {
                if (connected.contains(ch.legacyTarget)) {
                    received.computeIfAbsent(ch.legacyTarget, k -> new AtomicInteger()).incrementAndGet();
                }
                return;
            }
            for (UUID t : ch.targets) {
                if (connected.contains(t)) {
                    received.computeIfAbsent(t, k -> new AtomicInteger()).incrementAndGet();
                }
            }
        }

        private Object categoryBuilder() {
            Map<String, Object> fields = new LinkedHashMap<>();
            Object[] self = new Object[1];
            self[0] = proxy(VolumeCategory.Builder.class, (p, m, args) -> {
                String n = m.getName();
                if (n.equals("build")) {
                    String id = (String) fields.get("id");
                    if (id == null || !Pattern.matches("^[a-z_]{1,16}$", id)) {
                        // 和 SVC 的 VolumeCategoryImpl 一样
                        throw new IllegalArgumentException("Volume category ID can only contain a-z and _ with a maximum amount of 16 characters");
                    }
                    return Proxy.newProxyInstance(FakeSvc.class.getClassLoader(), new Class<?>[]{VolumeCategory.class},
                            new CategoryHandle(new LinkedHashMap<>(fields)));
                }
                if (n.startsWith("set") && args != null && args.length == 1) {
                    String key = Character.toLowerCase(n.charAt(3)) + n.substring(4);
                    fields.put(key, args[0]);
                    return self[0];
                }
                return unsupported(m);
            });
            return self[0];
        }

        private Object unsupported(Method m) {
            unexpected.add(m.getDeclaringClass().getSimpleName() + "." + m.getName());
            throw new UnsupportedOperationException("devtest 的假 SVC 没有模拟 " + m.getDeclaringClass().getSimpleName() + "." + m.getName());
        }
    }

    static final class CategoryHandle implements InvocationHandler {
        final Map<String, Object> fields;

        CategoryHandle(Map<String, Object> fields) {
            this.fields = fields;
        }

        @Override
        public Object invoke(Object proxy, Method m, Object[] args) {
            switch (m.getName()) {
                case "getId":
                    return fields.get("id");
                case "getName":
                    return fields.get("name");
                case "getDescription":
                    return fields.get("description");
                case "getIcon":
                    return fields.get("icon");
                case "getNameTranslationKey":
                    return fields.get("nameTranslationKey");
                case "getDescriptionTranslationKey":
                    return fields.get("descriptionTranslationKey");
                case "toString":
                    return "VolumeCategory" + fields;
                case "hashCode":
                    return System.identityHashCode(proxy);
                case "equals":
                    return proxy == args[0];
                default:
                    throw new UnsupportedOperationException(m.getName());
            }
        }
    }

    static final class Events {
        final Map<Class<?>, List<Consumer<Object>>> handlers = new LinkedHashMap<>();

        @SuppressWarnings("unchecked")
        EventRegistration registration() {
            return proxy(EventRegistration.class, (p, m, args) -> {
                if (m.getName().equals("registerEvent")) {
                    handlers.computeIfAbsent((Class<?>) args[0], k -> new ArrayList<>()).add((Consumer<Object>) args[1]);
                    return null;
                }
                throw new UnsupportedOperationException(m.getName());
            });
        }

        List<String> names() {
            List<String> l = new ArrayList<>();
            for (Class<?> c : handlers.keySet()) {
                l.add(c.getSimpleName());
            }
            return l;
        }

        void fire(Class<?> type, Object event) {
            for (Consumer<Object> c : handlers.getOrDefault(type, Collections.emptyList())) {
                try {
                    c.accept(event);
                } catch (Throwable t) {
                    // SVC 的 PluginManager.dispatchEvent 也是这样：记录下来，不往外抛
                    System.out.println("  事件处理抛出了异常（SVC 会记录成 Failed to dispatch event）：" + t);
                    t.printStackTrace(System.out);
                    failures++;
                }
            }
        }
    }

    @SuppressWarnings("unchecked")
    static <T> T proxy(Class<T> type, InvocationHandler h) {
        return (T) Proxy.newProxyInstance(FakeSvc.class.getClassLoader(), new Class<?>[]{type}, (p, m, args) -> {
            if (m.getDeclaringClass() == Object.class) {
                switch (m.getName()) {
                    case "toString":
                        return type.getSimpleName() + "@devtest";
                    case "hashCode":
                        return System.identityHashCode(p);
                    case "equals":
                        return p == args[0];
                    default:
                        break;
                }
            }
            if (m.isDefault()) {
                return InvocationHandler.invokeDefault(p, m, args);
            }
            return h.invoke(p, m, args);
        });
    }

    // ===================================================================================
    // 按 FML 的方式扫描 class 文件里的注解（FML 用 ASM 读 class 文件，不加载类）

    static List<String> scanForPlugins(Path jar) throws IOException {
        List<String> found = new ArrayList<>();
        try (ZipFile z = new ZipFile(jar.toFile())) {
            for (ZipEntry e : Collections.list(z.entries())) {
                if (!e.getName().endsWith(".class")) {
                    continue;
                }
                byte[] b;
                try (InputStream in = z.getInputStream(e)) {
                    b = in.readAllBytes();
                }
                ClassInfo info = ClassInfo.parse(b);
                if (info.major != 52) {
                    check(false, e.getName() + " 的 class 版本号是 " + info.major + "，不是 52");
                }
                if (info.annotations.contains(PLUGIN_ANNOTATION)) {
                    found.add(info.name.replace('/', '.'));
                }
            }
        }
        return found;
    }

    static final class ClassInfo {
        int major;
        String name;
        final List<String> annotations = new ArrayList<>();

        static ClassInfo parse(byte[] bytes) throws IOException {
            DataInputStream in = new DataInputStream(new java.io.ByteArrayInputStream(bytes));
            ClassInfo ci = new ClassInfo();
            if (in.readInt() != 0xCAFEBABE) {
                throw new IOException("不是 class 文件");
            }
            in.readUnsignedShort();
            ci.major = in.readUnsignedShort();
            int count = in.readUnsignedShort();
            String[] utf8 = new String[count];
            int[] classIndex = new int[count];
            for (int i = 1; i < count; i++) {
                int tag = in.readUnsignedByte();
                switch (tag) {
                    case 1:
                        utf8[i] = in.readUTF();
                        break;
                    case 7:
                        classIndex[i] = in.readUnsignedShort();
                        break;
                    case 8: case 16: case 19: case 20:
                        in.readUnsignedShort();
                        break;
                    case 3: case 4: case 9: case 10: case 11: case 12: case 17: case 18:
                        in.readInt();
                        break;
                    case 5: case 6:
                        in.readLong();
                        i++;
                        break;
                    case 15:
                        in.readUnsignedByte();
                        in.readUnsignedShort();
                        break;
                    default:
                        throw new IOException("不认识的常量池类型 " + tag);
                }
            }
            in.readUnsignedShort();
            ci.name = utf8[classIndex[in.readUnsignedShort()]];
            in.readUnsignedShort();
            int interfaces = in.readUnsignedShort();
            for (int i = 0; i < interfaces; i++) {
                in.readUnsignedShort();
            }
            for (int pass = 0; pass < 2; pass++) { // 字段，然后方法
                int members = in.readUnsignedShort();
                for (int i = 0; i < members; i++) {
                    in.readUnsignedShort();
                    in.readUnsignedShort();
                    in.readUnsignedShort();
                    skipAttributes(in);
                }
            }
            int attributes = in.readUnsignedShort();
            for (int i = 0; i < attributes; i++) {
                String attr = utf8[in.readUnsignedShort()];
                int length = in.readInt();
                if ("RuntimeVisibleAnnotations".equals(attr) || "RuntimeInvisibleAnnotations".equals(attr)) {
                    int n = in.readUnsignedShort();
                    for (int k = 0; k < n; k++) {
                        ci.annotations.add(utf8[in.readUnsignedShort()]);
                        skipAnnotationBody(in);
                    }
                } else {
                    in.skipNBytes(length);
                }
            }
            return ci;
        }

        private static void skipAttributes(DataInputStream in) throws IOException {
            int n = in.readUnsignedShort();
            for (int i = 0; i < n; i++) {
                in.readUnsignedShort();
                in.skipNBytes(in.readInt() & 0xFFFFFFFFL);
            }
        }

        private static void skipAnnotationBody(DataInputStream in) throws IOException {
            int pairs = in.readUnsignedShort();
            for (int i = 0; i < pairs; i++) {
                in.readUnsignedShort();
                skipElementValue(in);
            }
        }

        private static void skipElementValue(DataInputStream in) throws IOException {
            int tag = in.readUnsignedByte();
            switch (tag) {
                case 'e':
                    in.readUnsignedShort();
                    in.readUnsignedShort();
                    break;
                case '@':
                    in.readUnsignedShort();
                    skipAnnotationBody(in);
                    break;
                case '[': {
                    int n = in.readUnsignedShort();
                    for (int i = 0; i < n; i++) {
                        skipElementValue(in);
                    }
                    break;
                }
                default:
                    in.readUnsignedShort();
                    break;
            }
        }
    }

    // ===================================================================================
    // testsend 和裸协议

    static final class Result {
        final int code;
        final String out;

        Result(int code, String out) {
            this.code = code;
            this.out = out;
        }
    }

    static final class Proc {
        final Process process;
        final StringBuffer out = new StringBuffer();
        final Thread pump;

        Proc(List<String> command) throws IOException {
            process = new ProcessBuilder(command).redirectErrorStream(true).start();
            pump = new Thread(() -> {
                try (BufferedReader r = new BufferedReader(new InputStreamReader(process.getInputStream(), StandardCharsets.UTF_8))) {
                    String line;
                    while ((line = r.readLine()) != null) {
                        out.append(line).append('\n');
                        System.out.println("    [testsend] " + line);
                    }
                } catch (IOException ignored) {
                    // 进程结束
                }
            });
            pump.setDaemon(true);
            pump.start();
        }

        String output() {
            return out.toString();
        }

        void kill() {
            process.destroyForcibly();
        }

        /** 像按 Ctrl+C 那样请它退出（SIGTERM）。不用 Process.destroy()：它会顺手关掉输出流，之后打印的就收不到了。 */
        void term() {
            process.toHandle().destroy();
        }

        Result waitFor(long ms) throws InterruptedException {
            boolean done = process.waitFor(ms, TimeUnit.MILLISECONDS);
            if (!done) {
                process.destroyForcibly();
                process.waitFor(2, TimeUnit.SECONDS);
            }
            pump.join(2000);
            return new Result(done ? process.exitValue() : -1, out.toString());
        }
    }

    static Proc start(String testsend, String... args) throws IOException {
        List<String> cmd = new ArrayList<>();
        cmd.add(testsend);
        cmd.addAll(List.of(args));
        System.out.println("  $ testsend " + String.join(" ", args));
        return new Proc(cmd);
    }

    static Result run(String testsend, String... args) throws Exception {
        return start(testsend, args).waitFor(20000);
    }

    /** 发一个 HELLO，返回 ERROR 码；握手成功返回 0。 */
    static int rawHello(int port, String token, int version) throws IOException {
        try (Socket s = new Socket()) {
            s.connect(new InetSocketAddress("127.0.0.1", port), 2000);
            s.setSoTimeout(3000);
            OutputStream out = s.getOutputStream();
            out.write(frame(0x01, hello(version, token, version == 1 ? "房主" : "raw")));
            int[] code = new int[1];
            int type = readFrame(s.getInputStream(), code);
            if (type == 0x02) {
                out.write(frame(0x40, "bye".getBytes(StandardCharsets.UTF_8)));
                return 0;
            }
            return type == 0x03 ? code[0] : -1;
        }
    }

    static int rawBadPcm(int port, String token) throws IOException {
        try (Socket s = new Socket()) {
            s.connect(new InetSocketAddress("127.0.0.1", port), 2000);
            s.setSoTimeout(3000);
            OutputStream out = s.getOutputStream();
            out.write(frame(0x01, hello(1, token, "raw")));
            int[] code = new int[1];
            if (readFrame(s.getInputStream(), code) != 0x02) {
                return -1;
            }
            out.write(frame(0x10, new byte[10]));
            while (true) {
                int type = readFrame(s.getInputStream(), code);
                if (type == 0x03) {
                    return code[0];
                }
                if (type < 0) {
                    return -1;
                }
            }
        }
    }

    static int rawFirstFrameNotHello(int port) throws IOException {
        try (Socket s = new Socket()) {
            s.connect(new InetSocketAddress("127.0.0.1", port), 2000);
            s.setSoTimeout(3000);
            s.getOutputStream().write(frame(0x20, new byte[8]));
            int[] code = new int[1];
            return readFrame(s.getInputStream(), code) == 0x03 ? code[0] : -1;
        }
    }

    static byte[] hello(int version, String token, String name) {
        byte[] t = token.getBytes(StandardCharsets.UTF_8);
        byte[] n = name.getBytes(StandardCharsets.UTF_8);
        byte[] p = new byte[3 + t.length + n.length];
        p[0] = (byte) version;
        p[1] = (byte) (version >>> 8);
        p[2] = (byte) t.length;
        System.arraycopy(t, 0, p, 3, t.length);
        System.arraycopy(n, 0, p, 3 + t.length, n.length);
        return p;
    }

    static byte[] frame(int type, byte[] payload) {
        byte[] f = new byte[4 + payload.length];
        f[0] = (byte) type;
        f[2] = (byte) payload.length;
        f[3] = (byte) (payload.length >>> 8);
        System.arraycopy(payload, 0, f, 4, payload.length);
        return f;
    }

    /** 读一帧，返回类型；如果是 ERROR，把错误码放进 code[0]。连接断开返回 -1。 */
    static int readFrame(InputStream in, int[] code) throws IOException {
        DataInputStream d = new DataInputStream(in);
        byte[] h = new byte[4];
        try {
            d.readFully(h);
        } catch (IOException e) {
            return -1;
        }
        int len = (h[2] & 0xFF) | (h[3] & 0xFF) << 8;
        byte[] p = new byte[len];
        d.readFully(p);
        if ((h[0] & 0xFF) == 0x03 && len >= 2) {
            code[0] = (p[0] & 0xFF) | (p[1] & 0xFF) << 8;
        }
        return h[0] & 0xFF;
    }

    /** 只发不收的发送端：握手后不停地发 4 KB 的 PING，从不读模组的回复。 */
    static final class Flood {
        final Socket socket = new Socket();
        final boolean handshakeOk;
        final Thread thread;
        volatile long sentFrames;
        volatile String endedBy;

        Flood(int port, String token) throws IOException {
            socket.connect(new InetSocketAddress("127.0.0.1", port), 2000);
            socket.setSoTimeout(3000);
            OutputStream out = socket.getOutputStream();
            out.write(frame(0x01, hello(1, token, "flood")));
            int[] code = new int[1];
            handshakeOk = readFrame(socket.getInputStream(), code) == 0x02;
            byte[] ping = frame(0x20, new byte[4096]);
            thread = new Thread(() -> {
                try {
                    while (true) {
                        out.write(ping);
                        sentFrames++;
                    }
                } catch (IOException e) {
                    endedBy = e.toString();
                }
            }, "devtest-flood");
            thread.setDaemon(true);
            if (handshakeOk) {
                thread.start();
            }
        }

        boolean waitEnded(long ms) throws InterruptedException {
            thread.join(ms);
            return !thread.isAlive() && endedBy != null;
        }

        String describe() {
            return "发了 " + sentFrames + " 帧，" + (endedBy != null ? "写入失败：" + endedBy : "仍在写");
        }

        void close() throws InterruptedException {
            try {
                socket.close();
            } catch (IOException ignored) {
                // 已经关了
            }
            thread.join(3000);
        }
    }

    /** 模组当前的听众（读模组自己记的那张表，两种推送方式都适用）；语音服务器没在运行时为空集合。 */
    @SuppressWarnings("unchecked")
    static Set<UUID> listeners(Object plugin) {
        try {
            Object bridge = field(plugin, "current");
            return bridge == null ? Set.of() : new HashSet<>((Set<UUID>) field(field(bridge, "listeners"), "targets"));
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException(e);
        }
    }

    static Object field(Object target, String name) throws ReflectiveOperationException {
        java.lang.reflect.Field f = target.getClass().getDeclaredField(name);
        f.setAccessible(true);
        return f.get(target);
    }

    static Object invoke(Object target, String name, Class<?>[] types, Object... args) throws ReflectiveOperationException {
        Method m = target.getClass().getDeclaredMethod(name, types);
        m.setAccessible(true);
        return m.invoke(target, args);
    }

    static Object invokeStatic(String className, String name, Class<?>[] types, Object... args) throws ReflectiveOperationException {
        Method m = Class.forName(className).getDeclaredMethod(name, types);
        m.setAccessible(true);
        return m.invoke(null, args);
    }

    // ===================================================================================
    // 杂项

    static void writeConfig(Path file, boolean enabled, int port, String token, String exclude, boolean selftest) throws IOException {
        Files.createDirectories(file.getParent());
        String text = "enabled=" + enabled + "\nport=" + port + "\ntoken=" + token + "\nexclude=" + exclude
                + "\nselftest=" + selftest + "\nbypass_group_isolation=true\n";
        // 和建议 MineDeck 采用的写法一样：先写临时文件再改名，模组不会读到写了一半的文件
        Path tmp = file.resolveSibling(file.getFileName() + ".tmp");
        Files.write(tmp, text.getBytes(StandardCharsets.UTF_8));
        Files.move(tmp, file, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE);
        System.out.println("  （已写入配置：enabled=" + enabled + " port=" + port + " exclude=" + exclude + " selftest=" + selftest + "）");
    }

    static Properties loadProperties(Path file) throws IOException {
        Properties p = new Properties();
        try (InputStream in = Files.newInputStream(file)) {
            p.load(new InputStreamReader(in, StandardCharsets.UTF_8));
        }
        return p;
    }

    static int freePort() throws IOException {
        try (ServerSocket s = new ServerSocket(0)) {
            return s.getLocalPort();
        }
    }

    static boolean portOpen(int port) {
        try (Socket s = new Socket()) {
            s.connect(new InetSocketAddress("127.0.0.1", port), 300);
            return true;
        } catch (IOException e) {
            return false;
        }
    }

    static int count(String text, String part) {
        int n = 0;
        for (int i = text.indexOf(part); i >= 0; i = text.indexOf(part, i + part.length())) {
            n++;
        }
        return n;
    }

    static boolean threadAlive(String name) {
        for (Thread t : Thread.getAllStackTraces().keySet()) {
            if (t.getName().equals(name) && t.isAlive()) {
                return true;
            }
        }
        return false;
    }

    static boolean hasMethod(Class<?> type, String name, Class<?>... params) {
        try {
            type.getMethod(name, params);
            return true;
        } catch (NoSuchMethodException e) {
            return false;
        }
    }

    static boolean waitUntil(long ms, BooleanSupplier condition) {
        long end = System.currentTimeMillis() + ms;
        while (System.currentTimeMillis() < end) {
            if (condition.getAsBoolean()) {
                return true;
            }
            sleepQuietly(50);
        }
        return condition.getAsBoolean();
    }

    static void sleepQuietly(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }

    static void section(String title) {
        System.out.println();
        System.out.println("== " + title);
    }

    static void check(boolean ok, String what) {
        if (ok) {
            passes++;
            System.out.println("  [通过] " + what);
        } else {
            failures++;
            System.out.println("  [失败] " + what);
        }
    }
}
