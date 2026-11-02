package minedeck.companion;

import java.nio.file.Path;
import java.security.SecureRandom;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Iterator;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.TreeMap;
import java.util.UUID;
import java.util.function.Predicate;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonNull;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.google.gson.JsonPrimitive;
import com.mojang.authlib.GameProfile;
import com.player2.playerengine.PlayerEngineController;
import com.player2.playerengine.automaton.api.entity.LivingEntityInventory;
import com.player2.playerengine.commands.BlockScanner;
import com.player2.playerengine.commands.base.CommandException;
import com.player2.playerengine.commands.base.ItemList;
import com.player2.playerengine.tasks.base.Task;

import minedeck.companion.net.Args;
import minedeck.companion.net.CompanionConfig;
import minedeck.companion.net.ControlServer;
import minedeck.companion.net.Housekeeper;
import minedeck.companion.net.Protocol;
import minedeck.companion.net.Zones;

import net.minecraft.ChatFormatting;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.network.chat.Component;
import net.minecraft.network.chat.HoverEvent;
import net.minecraft.network.chat.MutableComponent;
import net.minecraft.resources.ResourceKey;
import net.minecraft.resources.ResourceLocation;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.level.TicketType;
import net.minecraft.tags.TagKey;
import net.minecraft.util.Mth;
import net.minecraft.world.damagesource.DamageSource;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.ai.attributes.DefaultAttributes;
import net.minecraft.world.item.Item;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.item.Items;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.Level;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.phys.Vec3;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * 伙伴模组的服务端核心：执行 MineDeck 发来的指令、管理多个伙伴的编号和任务、发事件、接管存档里的伙伴、
 * 清理强制加载的区块。协议见 companion/README.md 的「协议 v1」。
 *
 * <p>线程：网络线程只把指令转交过来（{@link #onRequest}），所有对世界的操作都在服务器主线程上做；WELCOME、STATUS
 * 读主线程每秒更新一次的快照。配置由定时检查线程读进来（{@link #onConfig}），区域等不可变对象直接换掉。
 *
 * <p>NeoForge 1.21.1 版和 Forge 1.20.1 版共用这份源码（companion/shared/），两个版本不同的地方在各自工程的
 * ServerCompanion、CompanionRegistry、Compat 里。
 */
public final class CompanionManager implements ControlServer.Handler, Housekeeper.Listener {

    public static final Logger LOG = LoggerFactory.getLogger("minedeck_companion");

    static final long STUCK_MS = 30_000L;
    static final double STUCK_MOVE_SQ = 1.5 * 1.5;
    static final long RECONCILE_AFTER_MS = 30_000L;
    static final long PROBE_MS = 10_000L;
    static final int DEFAULT_COLLECT_TIMEOUT = 600;
    static final int DEFAULT_TIMEOUT = 300;
    static final int ATTACK_DURATION_GRACE = 30;
    static final String DEFAULT_NAME = "伙伴";
    static final List<String> OPS = List.of("spawn", "despawn", "status", "collect", "follow", "goto", "attack", "give",
            "stop", "say");
    static final List<String> EVENTS = List.of("spawned", "adopted", "removed", "died", "task_started",
            "task_finished", "stuck", "config_applied");
    static final List<String> FEATURES = List.of("nodig", "adopt");

    /** 找存档里还没加载的伙伴时，临时加载它最后所在区块用的票（自己会过期，不会存进存档）。 */
    static final TicketType<ChunkPos> PROBE_TICKET = TicketType.create("minedeck_companion_probe",
            Comparator.comparingLong(ChunkPos::toLong), 20 * 15);

    private static volatile CompanionManager instance;

    final MinecraftServer server;
    final String session;
    final String modVersion;
    final String engineVersion;
    final ControlServer control;
    final Housekeeper housekeeper;
    private volatile CompanionConfig config = CompanionConfig.disabled();
    private volatile String configJson = "{}";
    private volatile String companionsJson = "[]";

    // 以下只在服务器主线程上读写
    private final Map<Integer, CompanionState> byId = new TreeMap<Integer, CompanionState>();
    private final Map<UUID, CompanionState> byUuid = new HashMap<UUID, CompanionState>();
    private final ArrayDeque<Runnable> deferred = new ArrayDeque<Runnable>();
    private final Map<UUID, Long> probes = new HashMap<UUID, Long>();
    private final long startedAt = System.currentTimeMillis();
    private CompanionRegistry registry;
    /** 开服时就强制加载、又不在任何伙伴记录里的区块（管理员 /forceload 的），按维度；这些永远不放开。 */
    private final Map<String, Set<Long>> adminForced = new HashMap<String, Set<Long>>();
    private long nextTaskId = 1;
    private int ticks;
    private boolean reconciled;
    private boolean snapshotDirty = true;

    private CompanionManager(MinecraftServer server, String modVersion) {
        this.server = server;
        this.modVersion = modVersion;
        this.engineVersion = Compat.modVersion("playerengine");
        byte[] raw = new byte[4];
        new SecureRandom().nextBytes(raw);
        StringBuilder sb = new StringBuilder();
        for (byte b : raw) {
            sb.append(String.format("%02x", b & 0xFF));
        }
        this.session = sb.toString();
        this.control = new ControlServer(this, LOG);
        Path file = CompanionConfig.path(Compat.gameDir());
        this.housekeeper = new Housekeeper(file, control, this, LOG);
    }

    // ---------------------------------------------------------------- 生命周期（加载器的事件里调用）

    /** 开服之后（主线程）。 */
    public static void start(MinecraftServer server, String modVersion) {
        CompanionManager m = new CompanionManager(server, modVersion);
        m.registry = CompanionRegistry.get(server);
        for (ServerLevel level : server.getAllLevels()) {
            Set<Long> admin = new HashSet<Long>(level.getForcedChunks());
            String dim = level.dimension().location().toString();
            for (RegistryEntry en : m.registry.entries.values()) {
                if (dim.equals(en.dim)) {
                    for (long v : en.chunks) {
                        admin.remove(v);
                    }
                }
            }
            if (!admin.isEmpty()) {
                m.adminForced.put(dim, admin);
            }
        }
        instance = m;
        // 存档里已经加载了的伙伴可能在开服过程中就 tick 过了，那时还没有管理器，现在补上
        for (ServerLevel level : server.getAllLevels()) {
            for (Entity e : level.getAllEntities()) {
                if (e instanceof ServerCompanion c && c.isAlive()) {
                    adopt(c);
                }
            }
        }
        m.housekeeper.start();
        LOG.info("MineDeck 伙伴模组已就绪：存档里记着 {} 个伙伴，已接管 {} 个（PlayerEngine {}）", m.registry.entries.size(),
                m.byId.size(), m.engineVersion);
    }

    /** 开始关服时（主线程）：先停定时检查，再关端口；伙伴留在存档里，下次开服接管。 */
    public static void stop() {
        CompanionManager m = instance;
        if (m == null) {
            return;
        }
        m.housekeeper.stop();
        m.control.close("服务器正在关闭");
        for (CompanionState st : m.byId.values()) {
            ServerCompanion e = st.live();
            if (e != null) {
                m.updateRegistry(st, e);
            }
        }
        instance = null;
        markForcedChunksDirty(m.server);
        stopScanners(m.server);
    }

    /** 关服结束时（主线程）：PlayerEngine 的方块扫描线程还在的话记一条，说明进程为什么退不出去。 */
    public static void stopped() {
        List<Thread> left = scannerThreads();
        if (!left.isEmpty()) {
            LOG.warn("关服后还有 {} 个 PlayerEngine 方块扫描线程没结束（{}），Java 进程可能退不出去，需要手动结束", left.size(),
                    threadNames(left));
        }
    }

    /**
     * PlayerEngine 的方块扫描跑在普通（非守护）线程上，扫描时在那个线程上调 getChunk，要等主线程替它取区块。
     * 关服时正好在扫的话，主线程不再处理这些请求，那个线程就一直等下去，Java 进程退不出去。
     * 所以在开始关服时（主线程，区块系统还正常）让每个伙伴的扫描停下（reset 会让扫描循环尽快退出），
     * 同时替它们处理排着的区块请求，直到扫描线程都结束，最多等 5 秒。关服时伙伴已经不再 tick，不会再起新的扫描。
     */
    private static void stopScanners(MinecraftServer server) {
        int stopped = 0;
        for (ServerLevel level : server.getAllLevels()) {
            for (Entity e : level.getAllEntities()) {
                if (e instanceof ServerCompanion c && c.controller() != null) {
                    try {
                        c.controller().getBlockScanner().reset();
                        stopped++;
                    } catch (RuntimeException ex) {
                        LOG.warn("停伙伴「{}」的方块扫描时出错：{}", c.getName().getString(), ex.toString());
                    }
                }
            }
        }
        long start = System.nanoTime();
        long deadline = start + 5_000_000_000L;
        List<Thread> left = scannerThreads();
        boolean waited = !left.isEmpty();
        while (!left.isEmpty() && System.nanoTime() < deadline) {
            for (ServerLevel level : server.getAllLevels()) {
                try {
                    while (level.getChunkSource().pollTask()) {
                        // 处理排着的区块请求
                    }
                } catch (RuntimeException ex) {
                    LOG.warn("关服时处理区块请求出错：{}", ex.toString());
                }
            }
            try {
                Thread.sleep(10L);
            } catch (InterruptedException ex) {
                Thread.currentThread().interrupt();
                break;
            }
            left = scannerThreads();
        }
        if (!left.isEmpty()) {
            LOG.warn("等了 5 秒，还有 {} 个 PlayerEngine 方块扫描线程没停下（{}）", left.size(), threadNames(left));
        } else if (waited) {
            LOG.info("已停下 {} 个伙伴的方块扫描，等正在扫的线程结束用了 {} 毫秒", stopped,
                    (System.nanoTime() - start) / 1_000_000L);
        }
    }

    /** 还活着的、正在跑 PlayerEngine 方块扫描的非守护线程。 */
    static List<Thread> scannerThreads() {
        String scanner = BlockScanner.class.getName();
        List<Thread> out = new ArrayList<Thread>();
        for (Map.Entry<Thread, StackTraceElement[]> en : Thread.getAllStackTraces().entrySet()) {
            Thread t = en.getKey();
            if (t == Thread.currentThread() || t.isDaemon() || !t.isAlive()) {
                continue;
            }
            for (StackTraceElement f : en.getValue()) {
                if (scanner.equals(f.getClassName())) {
                    out.add(t);
                    break;
                }
            }
        }
        return out;
    }

    private static String threadNames(List<Thread> threads) {
        StringBuilder b = new StringBuilder();
        for (Thread t : threads) {
            if (b.length() > 0) {
                b.append("、");
            }
            b.append(t.getName());
        }
        return b.toString();
    }

    static CompanionManager current() {
        return instance;
    }

    /** 现在的禁挖区域（任意线程）。 */
    static Zones zones() {
        CompanionManager m = instance;
        return m == null ? Zones.NONE : m.config.zones;
    }

    /** 伙伴正在做 give 时接东西的玩家（在线的话），否则 null。主线程上调用（伙伴扔东西时）。 */
    static ServerPlayer giveTarget(ServerCompanion e) {
        CompanionManager m = instance;
        CompanionState st = m == null ? null : m.byUuid.get(e.getUUID());
        TaskRun t = st == null ? null : st.task;
        if (t == null || t.done || !"give".equals(t.op) || t.player == null) {
            return null;
        }
        return m.server.getPlayerList().getPlayerByName(t.player);
    }

    /** 这种生物在配置的 attack_deny 里（伙伴打不伤它）。 */
    static boolean attackDenied(Entity e) {
        CompanionManager m = instance;
        return m != null && !m.config.attackDeny.isEmpty()
                && m.config.attackDeny.contains(BuiltInRegistries.ENTITY_TYPE.getKey(e.getType()).toString());
    }

    /** 每个服务器 tick 结束时（主线程）。 */
    public static void tick() {
        CompanionManager m = instance;
        if (m == null) {
            return;
        }
        m.ticks++;
        m.drainDeferred();
        long now = System.currentTimeMillis();
        if (m.ticks % 5 == 0) {
            for (CompanionState st : new ArrayList<CompanionState>(m.byId.values())) {
                ServerCompanion e = st.live();
                PlayerEngineController c = e == null ? null : e.controller();
                if (c != null) {
                    try {
                        e.guard().tick(c, now);
                    } catch (RuntimeException ex) {
                        LOG.warn("检查伙伴的路径时出错：{}", ex.toString());
                    }
                }
            }
        }
        if (m.ticks % 20 == 0) {
            m.perSecond(now);
        }
        if (m.snapshotDirty) {
            m.refreshSnapshot();
        }
    }

    private void later(Runnable r) {
        deferred.addLast(r);
    }

    private void drainDeferred() {
        for (int n = deferred.size(); n > 0 && !deferred.isEmpty(); n--) {
            try {
                deferred.removeFirst().run();
            } catch (RuntimeException e) {
                LOG.error("伙伴模组的延后处理出错", e);
            }
        }
    }

    // ---------------------------------------------------------------- 配置

    @Override
    public void onConfig(CompanionConfig cfg, boolean first) {
        config = cfg;
        configJson = Json.str(Json.fromJava(cfg.summary()));
        if (!first) {
            server.execute(() -> {
                JsonObject ev = Json.event("config_applied");
                ev.add("config", JsonParser.parseString(configJson));
                control.event(Json.str(ev));
            });
        }
    }

    // ---------------------------------------------------------------- ControlServer.Handler

    @Override
    public void onRequest(String json, ControlServer.Connection conn) {
        server.execute(() -> handle(json, conn));
    }

    @Override
    public void onMainThread(Runnable task) {
        server.execute(task);
    }

    /** 在主线程上调用（ControlServer 握手时经 onMainThread 安排）：先刷新快照，读的是这一刻的伙伴和任务。 */
    @Override
    public String welcomeJson(int version) {
        refreshSnapshotIfDirty();
        JsonObject o = new JsonObject();
        o.addProperty("protocol", version);
        o.addProperty("session", session);
        o.addProperty("mod", modVersion);
        o.addProperty("loader", Compat.LOADER);
        o.addProperty("minecraft", server.getServerVersion());
        o.addProperty("playerEngine", engineVersion);
        o.add("ops", strings(OPS));
        o.add("events", strings(EVENTS));
        o.add("features", strings(FEATURES));
        JsonObject limits = new JsonObject();
        limits.addProperty("maxCount", Args.MAX_COUNT);
        limits.addProperty("maxText", Args.MAX_TEXT);
        limits.addProperty("maxName", Args.MAX_NAME);
        limits.addProperty("maxZones", CompanionConfig.MAX_ZONES);
        o.add("limits", limits);
        o.add("config", JsonParser.parseString(configJson));
        o.add("companions", JsonParser.parseString(companionsJson));
        return Json.str(o);
    }

    @Override
    public String statusJson() {
        return "{\"companions\":" + companionsJson + "}";
    }

    private static JsonArray strings(List<String> list) {
        JsonArray a = new JsonArray();
        for (String s : list) {
            a.add(s);
        }
        return a;
    }

    // ---------------------------------------------------------------- 指令

    private void handle(String json, ControlServer.Connection conn) {
        JsonElement id = JsonNull.INSTANCE;
        String op = "";
        try {
            JsonObject req;
            try {
                JsonElement parsed = JsonParser.parseString(json);
                if (!parsed.isJsonObject()) {
                    throw new OpError("bad_json", "指令要是一个 JSON 对象");
                }
                req = parsed.getAsJsonObject();
            } catch (RuntimeException e) {
                if (e instanceof OpError) {
                    throw e;
                }
                throw new OpError("bad_json", "指令不是合法的 JSON：" + e.getMessage());
            }
            JsonElement idEl = req.get("id");
            if (idEl != null && idEl.isJsonPrimitive() && idEl.getAsJsonPrimitive().isNumber()) {
                id = idEl;
            }
            if (id.isJsonNull() || id.getAsDouble() != Math.rint(id.getAsDouble())) {
                throw new OpError("bad_json", "缺少整数的 id");
            }
            JsonElement opEl = req.get("op");
            if (opEl == null || !opEl.isJsonPrimitive() || !opEl.getAsJsonPrimitive().isString()) {
                throw new OpError("bad_json", "缺少 op");
            }
            op = opEl.getAsString();
            JsonObject res;
            switch (op) {
                case "spawn":
                    res = spawn(req);
                    break;
                case "despawn":
                    res = despawn(req);
                    break;
                case "status":
                    res = status(req);
                    break;
                case "collect":
                    res = collect(req);
                    break;
                case "follow":
                    res = follow(req);
                    break;
                case "goto":
                    res = goTo(req);
                    break;
                case "attack":
                    res = attack(req);
                    break;
                case "give":
                    res = give(req);
                    break;
                case "stop":
                    res = stopTask(req);
                    break;
                case "say":
                    res = say(req);
                    break;
                default:
                    throw new OpError("unknown_op", "不认识的指令：" + op);
            }
            JsonObject out = new JsonObject();
            out.add("id", id);
            out.addProperty("ok", true);
            for (Map.Entry<String, JsonElement> e : res.entrySet()) {
                out.add(e.getKey(), e.getValue());
            }
            conn.response(Json.str(out));
        } catch (OpError e) {
            JsonObject out = new JsonObject();
            out.add("id", id);
            out.addProperty("ok", false);
            out.addProperty("code", e.code);
            out.addProperty("error", e.getMessage());
            for (Map.Entry<String, JsonElement> x : e.extra.entrySet()) {
                out.add(x.getKey(), x.getValue());
            }
            conn.response(Json.str(out));
        } catch (RuntimeException e) {
            LOG.error("执行伙伴指令 {} 出错", op, e);
            JsonObject out = new JsonObject();
            out.add("id", id);
            out.addProperty("ok", false);
            out.addProperty("code", "internal");
            out.addProperty("error", "模组内部出错：" + e);
            conn.response(Json.str(out));
        }
        refreshSnapshotIfDirty();
    }

    // ---- spawn / despawn / status

    private JsonObject spawn(JsonObject req) {
        String name = DEFAULT_NAME;
        if (req.has("name")) {
            name = Args.checkName(optString(req, "name"));
            if (name == null) {
                throw OpError.badArgs("name 要有 1–" + Args.MAX_NAME + " 个字，不能有控制字符和 §");
            }
        }
        for (CompanionState st : byId.values()) {
            if (st.name != null && st.name.equalsIgnoreCase(name)) {
                throw new OpError("name_taken", "已经有一个叫「" + st.name + "」的伙伴了（编号 " + st.id + "）");
            }
        }
        for (RegistryEntry en : unadoptedEntries()) {
            if (en.name != null && en.name.equalsIgnoreCase(name)) {
                throw new OpError("name_taken", "存档里已经有一个叫「" + en.name + "」的伙伴（编号 " + en.id + "，还没加载）");
            }
        }
        String owner = req.has("owner") ? playerName(req, "owner") : null;
        String ownerUuid = null;
        if (owner != null) {
            ServerPlayer p = server.getPlayerList().getPlayerByName(owner);
            if (p != null) {
                ownerUuid = p.getUUID().toString();
                owner = p.getGameProfile().getName();
            } else if (server.getProfileCache() != null) {
                Optional<GameProfile> gp = server.getProfileCache().get(owner);
                if (gp.isPresent() && gp.get().getId() != null) {
                    ownerUuid = gp.get().getId().toString();
                }
            }
        }
        String tag = null;
        if (req.has("tag")) {
            tag = optString(req, "tag");
            if (tag == null || !Args.TAG.matcher(tag).matches()) {
                throw OpError.badArgs("tag 只能有 1–64 个字母、数字和 _ . : -");
            }
        }
        // 先查参数（bad_args、name_taken），再查数量上限
        CompanionConfig cfg = config;
        int known = byId.size() + unadoptedEntries().size();
        if (known >= cfg.maxCompanions) {
            throw new OpError("limit", "伙伴数量到上限了（最多 " + cfg.maxCompanions + " 个）").with("limit", cfg.maxCompanions);
        }

        ServerLevel level;
        double x;
        double y;
        double z;
        float yaw = 0;
        ServerPlayer near = null;
        if (req.has("near")) {
            String n = playerName(req, "near");
            near = server.getPlayerList().getPlayerByName(n);
            if (near == null) {
                throw new OpError("no_player", "玩家不在线：" + n);
            }
        } else if (!req.has("x") && !req.has("z") && owner != null) {
            near = server.getPlayerList().getPlayerByName(owner);
        }
        if (near != null) {
            level = near.serverLevel();
            yaw = near.getYRot();
            // 出生在玩家旁边（右手边 1.5 格），不要和玩家重叠
            double rad = Math.toRadians(near.getYRot() + 90);
            x = near.getX() - Math.sin(rad) * 1.5;
            z = near.getZ() + Math.cos(rad) * 1.5;
            y = near.getY();
        } else if (req.has("x") || req.has("z")) {
            int ix = coord(req, "x");
            int iz = coord(req, "z");
            level = server.overworld();
            if (req.has("dim")) {
                level = dimension(optString(req, "dim"));
            }
            x = ix + 0.5;
            z = iz + 0.5;
            y = req.has("y") ? yCoord(req, "y") : surface(level, x, z);
        } else {
            level = server.overworld();
            BlockPos s = level.getSharedSpawnPos();
            x = s.getX() + 0.5;
            z = s.getZ() + 0.5;
            y = surface(level, x, z);
        }

        Entity created = Compat.companionType().create(level);
        if (!(created instanceof ServerCompanion)) {
            throw new OpError("spawn_failed", "创建伙伴实体失败");
        }
        ServerCompanion e = (ServerCompanion) created;
        int id = allocateId();
        e.setMeta(id, owner, ownerUuid, tag);
        e.freshSpawn = true;
        e.moveTo(x, y, z, yaw, 0.0F);
        e.setYHeadRot(yaw);
        e.setCustomName(Component.literal(name));
        e.setCustomNameVisible(true);
        if (!level.addFreshEntity(e)) {
            throw new OpError("spawn_failed", "伙伴没能加进世界");
        }
        CompanionState st = register(e);
        JsonObject info = companionJson(st, false);
        JsonObject ev = Json.event("spawned");
        ev.addProperty("companion", st.id);
        ev.add("info", info);
        control.event(Json.str(ev));
        LOG.info("伙伴「{}」（编号 {}）已刷出：{} {} {}（{}）{}", name, id, fmt(x), fmt(y), fmt(z),
                level.dimension().location(), owner == null ? "" : "，主人 " + owner);
        JsonObject res = new JsonObject();
        res.addProperty("companion", st.id);
        res.add("info", info);
        return res;
    }

    private JsonObject despawn(JsonObject req) {
        List<CompanionState> targets = targets(req);
        boolean drop = optBool(req, "drop", false);
        JsonArray removed = new JsonArray();
        for (CompanionState st : targets) {
            ServerCompanion e = st.live();
            removed.add(st.id);
            if (e == null) {
                // 实体不在（理论上不会：伙伴所在的区块是强制加载的），只把记录删掉
                forget(st, "despawn", null);
                continue;
            }
            if (drop) {
                e.dropAllItems();
            }
            e.despawnRequested = true;
            e.discard();
        }
        JsonObject res = new JsonObject();
        res.add("removed", removed);
        return res;
    }

    private JsonObject status(JsonObject req) {
        boolean inventory = optBool(req, "inventory", false);
        JsonArray arr = new JsonArray();
        if (req.has("companion")) {
            arr.add(companionJson(resolve(req), inventory));
        } else {
            for (CompanionState st : byId.values()) {
                arr.add(companionJson(st, inventory));
            }
        }
        JsonObject res = new JsonObject();
        res.add("companions", arr);
        res.add("config", JsonParser.parseString(configJson));
        return res;
    }

    // ---- 任务

    private JsonObject collect(JsonObject req) {
        CompanionState st = resolve(req);
        String item = itemArg(req, "item");
        int count = intArg(req, "count", 1, 1, Args.MAX_COUNT);
        int timeout = timeoutArg(req, DEFAULT_COLLECT_TIMEOUT);
        JsonObject args = new JsonObject();
        args.addProperty("item", item);
        args.addProperty("count", count);
        TaskRun t = newTask(st, "collect", args, timeout, "get " + item + " " + count);
        t.item = item;
        t.want = count;
        t.before = countItem(requireReady(st), item);
        return runCommand(st, t);
    }

    private JsonObject follow(JsonObject req) {
        CompanionState st = resolve(req);
        String player = onlinePlayer(req, "player", st);
        int timeout = timeoutArgAllowZero(req, 0);
        JsonObject args = new JsonObject();
        args.addProperty("player", player);
        TaskRun t = newTask(st, "follow", args, timeout, "follow " + player);
        t.player = player;
        return runCommand(st, t);
    }

    private JsonObject goTo(JsonObject req) {
        CompanionState st = resolve(req);
        int x = coord(req, "x");
        int z = coord(req, "z");
        Integer y = req.has("y") ? yCoord(req, "y") : null;
        int timeout = timeoutArg(req, DEFAULT_TIMEOUT);
        JsonObject args = new JsonObject();
        args.addProperty("x", x);
        if (y != null) {
            args.addProperty("y", y);
        } else {
            args.add("y", JsonNull.INSTANCE);
        }
        args.addProperty("z", z);
        String command = y != null ? "goto " + x + " " + y + " " + z : "goto " + x + " " + z;
        return runCommand(st, newTask(st, "goto", args, timeout, command));
    }

    private JsonObject give(JsonObject req) {
        CompanionState st = resolve(req);
        String player = onlinePlayer(req, "player", st);
        String item = itemArg(req, "item");
        int count = intArg(req, "count", 1, 1, Args.MAX_COUNT);
        boolean fetch = optBool(req, "fetch", false);
        int timeout = timeoutArg(req, DEFAULT_TIMEOUT);
        ServerCompanion e = requireReady(st);
        int have = countItem(e, item);
        if (!fetch && have < count) {
            throw new OpError("not_enough", "背包里只有 " + Math.max(have, 0) + " 个 " + item + "，不够 " + count + " 个")
                    .with("have", Math.max(have, 0));
        }
        JsonObject args = new JsonObject();
        args.addProperty("player", player);
        args.addProperty("item", item);
        args.addProperty("count", count);
        args.addProperty("fetch", fetch);
        TaskRun t = newTask(st, "give", args, timeout, "give " + player + " " + Args.engineItemName(item) + " " + count);
        t.item = item;
        t.want = count;
        t.before = have;
        t.player = player;
        return runCommand(st, t);
    }

    private JsonObject attack(JsonObject req) {
        CompanionState st = resolve(req);
        String mob = Args.withNamespace(idArg(req, "mob"));
        ResourceLocation rl = Compat.tryParse(mob);
        Optional<EntityType<?>> type = rl == null ? Optional.<EntityType<?>>empty() : BuiltInRegistries.ENTITY_TYPE.getOptional(rl);
        if (type.isEmpty()) {
            throw OpError.badArgs("没有这种生物：" + mob);
        }
        CompanionConfig cfg = config;
        String why = AttackTask.forbiddenType(type.get(), mob, cfg.attackDeny);
        if (why != null) {
            throw new OpError("forbidden", why);
        }
        if (!isLivingType(type.get())) {
            throw OpError.badArgs(mob + " 不是生物");
        }
        Integer count = req.has("count") ? intArg(req, "count", 1, 1, Args.MAX_ATTACK_COUNT) : null;
        Integer duration = req.has("duration") ? intArg(req, "duration", 1, 1, Args.MAX_DURATION) : null;
        if (count == null && duration == null) {
            count = 1;
        }
        int radius = intArg(req, "radius", Args.DEFAULT_RADIUS, 1, Args.MAX_RADIUS);
        int defaultTimeout = count == null ? duration + ATTACK_DURATION_GRACE : DEFAULT_TIMEOUT;
        int timeout = timeoutArg(req, defaultTimeout);
        ServerCompanion e = requireReady(st);
        JsonObject args = new JsonObject();
        args.addProperty("mob", mob);
        args.add("count", count == null ? JsonNull.INSTANCE : new JsonPrimitive(count));
        args.add("duration", duration == null ? JsonNull.INSTANCE : new JsonPrimitive(duration));
        args.addProperty("radius", radius);
        TaskRun t = newTask(st, "attack", args, timeout, null);
        t.want = count == null ? 0 : count;
        long endAt = duration == null ? 0 : System.currentTimeMillis() + duration * 1000L;
        AttackTask task = new AttackTask(type.get(), mob, e.position(), radius, count == null ? 0 : count, endAt,
                cfg.attackDeny);
        t.attack = task;
        PlayerEngineController c = e.controller();
        e.guard().beforeTask(c);
        supersede(st);
        st.task = t;
        c.runUserTask(task, () -> later(() -> onPeTaskEnd(st, t, null, null)));
        return started(st, t);
    }

    private JsonObject stopTask(JsonObject req) {
        List<CompanionState> targets = targets(req);
        JsonArray stopped = new JsonArray();
        for (CompanionState st : targets) {
            TaskRun t = st.task;
            if (t != null) {
                stopped.add(t.id);
                finishTask(st, t, "cancelled", "MineDeck 让它停下");
            }
            ServerCompanion e = st.live();
            PlayerEngineController c = e == null ? null : e.controller();
            if (c != null) {
                c.stop();
                e.guard().restore(c, "停下了");
            }
        }
        JsonObject res = new JsonObject();
        res.add("stopped", stopped);
        return res;
    }

    private JsonObject say(JsonObject req) {
        CompanionState st = resolve(req);
        ServerCompanion e = st.live();
        if (e == null) {
            throw new OpError("no_companion", "伙伴「" + st.name + "」现在不在世界里");
        }
        String text = Args.checkText(optString(req, "text"));
        if (text == null) {
            throw OpError.badArgs("text 去掉 § 格式码和控制字符后要有 1–" + Args.MAX_TEXT + " 个字");
        }
        ServerPlayer to = null;
        if (req.has("to")) {
            String n = playerName(req, "to");
            to = server.getPlayerList().getPlayerByName(n);
            if (to == null) {
                throw new OpError("no_player", "玩家不在线：" + n);
            }
        }
        String name = e.getName().getString();
        MutableComponent nameC = Component.literal(name).withStyle(s -> s.withColor(ChatFormatting.AQUA)
                .withHoverEvent(new HoverEvent(HoverEvent.Action.SHOW_TEXT, Component.literal("MineDeck AI 伙伴"))));
        MutableComponent line = Component.literal("<").append(nameC).append("> ").append(Component.literal(text));
        // 不经过原版的聊天系统，控制台里不会出现 "<名字> 文本" 这种像玩家聊天的行
        if (to != null) {
            to.sendSystemMessage(Component.literal("（悄悄话）").withStyle(ChatFormatting.GRAY).append(line));
            LOG.info("伙伴「{}」悄悄对 {} 说：{}", name, to.getGameProfile().getName(), text);
        } else {
            for (ServerPlayer p : server.getPlayerList().getPlayers()) {
                p.sendSystemMessage(line);
            }
            LOG.info("伙伴「{}」说：{}", name, text);
        }
        return new JsonObject();
    }

    // ---- 任务的公共部分

    private TaskRun newTask(CompanionState st, String op, JsonObject args, int timeout, String command) {
        return new TaskRun(nextTaskId++, st.id, op, args, timeout, command);
    }

    /**
     * 把指令交给 PlayerEngine。PlayerEngine 当场不接受（物品名不认识之类）就回 rejected；开始执行后结束时发 task_finished。
     *
     * <p>伙伴手上有任务时，先把它停下、记成被顶掉，再发新指令。不能让 PlayerEngine 自己替换：它的每种指令只有一个对象，
     * 回调存在这个对象上；同一种指令（goto 换 goto、get 换 get……）替换时，它会把旧任务的「结束」通过这个对象报出去，
     * 结果报成了新任务马上做完，而新任务真正结束时又不再报（实测 goto 换 goto，新任务 0.1 秒就「做完了」）。
     */
    private JsonObject runCommand(CompanionState st, TaskRun t) {
        ServerCompanion e = requireReady(st);
        PlayerEngineController c = e.controller();
        if (t.item != null) {
            // 先用 PlayerEngine 自己的解析检查物品名（不认识时它的原话进 error），免得为一条不会被接受的指令停下旧任务
            try {
                ItemList.parseRemainder(t.item + " " + Math.max(1, t.want));
            } catch (CommandException ex) {
                t.done = true;
                throw new OpError("rejected", "PlayerEngine 不接受这条指令：" + String.valueOf(ex.getMessage()).trim());
            }
        }
        if (st.task != null && !st.task.done) {
            supersede(st);
            c.stop();
        }
        e.guard().beforeTask(c);
        String[] syncError = new String[1];
        boolean[] calling = {true};
        String line = c.getCommandExecutor().getCommandPrefix() + t.command;
        c.getCommandExecutor().execute(line,
                () -> st.task = t,
                () -> later(() -> onPeTaskEnd(st, t, null, null)),
                note -> later(() -> onPeTaskEnd(st, t, null, note)),
                (CommandException ex) -> {
                    if (calling[0]) {
                        // 指令还没开始执行就出错了（参数解析、检查），任务没装上
                        syncError[0] = String.valueOf(ex.getMessage());
                    } else {
                        later(() -> onPeTaskEnd(st, t, String.valueOf(ex.getMessage()), null));
                    }
                });
        calling[0] = false;
        if (syncError[0] != null || st.task != t) {
            t.done = true;
            if (st.task == t) {
                st.task = null;
            }
            throw new OpError("rejected", syncError[0] != null
                    ? "PlayerEngine 不接受这条指令：" + syncError[0].trim()
                    : "PlayerEngine 没有执行这条指令");
        }
        LOG.info("伙伴「{}」的任务 #{} 开始：{}", st.name, t.id, line);
        return started(st, t);
    }

    private JsonObject started(CompanionState st, TaskRun t) {
        st.refPos = null;
        st.stuckReported = false;
        st.nodigReported = false;
        JsonObject ev = Json.event("task_started");
        ev.addProperty("companion", st.id);
        ev.add("task", t.json());
        control.event(Json.str(ev));
        snapshotDirty = true;
        JsonObject res = new JsonObject();
        res.add("task", t.json());
        return res;
    }

    private void supersede(CompanionState st) {
        TaskRun old = st.task;
        if (old != null && !old.done) {
            finishTask(st, old, "superseded", "被新的指令替换");
        }
    }

    private void onPeTaskEnd(CompanionState st, TaskRun t, String error, String note) {
        if (t.done) {
            return;
        }
        if (error == null && t.player != null && server.getPlayerList().getPlayerByName(t.player) == null) {
            // PlayerEngine 的 follow / give 在玩家下线时自己结束（带一句说明），对 MineDeck 来说这是没做成
            finishTask(st, t, "failed", "玩家 " + t.player + " 下线了");
            return;
        }
        finishTask(st, t, error != null ? "failed" : "done", error != null ? error : note);
    }

    /** 记下任务结束，发 task_finished。可以重复调（已经结束的不会再发）。 */
    private void finishTask(CompanionState st, TaskRun t, String result, String detail) {
        if (t.done) {
            return;
        }
        t.done = true;
        if (st.task == t) {
            st.task = null;
        }
        ServerCompanion e = st.live();
        JsonObject ev = Json.event("task_finished");
        ev.addProperty("companion", st.id);
        ev.add("task", t.json());
        if ("collect".equals(t.op) && e != null) {
            int have = countItem(e, t.item);
            int gained = have - t.before;
            ev.addProperty("have", have);
            ev.addProperty("gained", gained);
            ev.addProperty("want", t.want);
            if ("done".equals(result) && have >= 0 && gained < t.want) {
                result = "incomplete";
            }
        } else if ("give".equals(t.op) && e != null) {
            int have = countItem(e, t.item);
            ev.addProperty("given", t.thrown);
            ev.addProperty("have", have);
            ev.addProperty("want", t.want);
            // PlayerEngine 交不出去（玩家不在附近、物品弄不到……）也只是结束任务，不算失败：按实际扔出去的个数判断。
            // detail 只写原因，个数在 given / want 里
            if ("done".equals(result) && t.thrown < t.want) {
                result = t.thrown == 0 ? "failed" : "incomplete";
                String why = giveShortfall(e, t, have);
                if (detail != null && !detail.isBlank()) {
                    why = why == null ? "PlayerEngine：" + clip(detail.trim(), 120) : why + "（PlayerEngine：" + clip(detail.trim(), 120) + "）";
                }
                detail = why != null ? why : t.thrown == 0 ? "PlayerEngine 没把东西交出去" : "PlayerEngine 没把剩下的交出去";
            }
        } else if ("attack".equals(t.op) && t.attack != null) {
            ev.addProperty("killed", t.attack.killed);
            ev.add("want", t.want > 0 ? new JsonPrimitive(t.want) : JsonNull.INSTANCE);
            if ("done".equals(result) && t.want > 0 && t.attack.killed < t.want) {
                result = "incomplete";
                if (detail == null) {
                    detail = "范围里没有能打的 " + t.attack.typeId + " 了";
                }
            }
        }
        ev.addProperty("result", result);
        if (detail != null && !detail.isBlank()) {
            ev.addProperty("detail", detail.trim());
        }
        ev.addProperty("seconds", Json.round((System.currentTimeMillis() - t.startedAt) / 1000.0));
        ev.add("pos", posJson(st));
        JsonObject last = ev.deepCopy();
        last.remove("event");
        last.remove("companion");
        last.addProperty("at", System.currentTimeMillis());
        st.lastResult = last;
        if (e != null && e.controller() != null) {
            e.guard().restore(e.controller(), "任务结束了");
        }
        String out = Json.str(ev);
        LOG.info("伙伴「{}」的任务 #{} 结束：{}", st.name, t.id, out);
        control.event(out);
        snapshotDirty = true;
    }

    // ---------------------------------------------------------------- 伙伴实体的回调（主线程）

    /** 伙伴实体第一次在服务端 tick 时调用（刷出的、存档里读回来的、换了维度的）。返回管理器里的记录。 */
    static CompanionState adopt(ServerCompanion e) {
        CompanionManager m = instance;
        if (m == null) {
            return null;
        }
        CompanionState st = m.byUuid.get(e.getUUID());
        if (st != null) {
            // 换维度后的新实体、或者重新加载的实体：沿用原来的编号和任务记录
            e.setMeta(st.id, st.owner, st.ownerUuid, st.tag);
            st.remember(e);
            m.snapshotDirty = true;
            return st;
        }
        RegistryEntry entry = m.registry.entries.get(e.getUUID());
        int id = entry != null ? entry.id : e.companionId();
        if (id <= 0 || m.byId.containsKey(id) || m.idUsedByOther(id, e.getUUID())) {
            int old = id;
            id = m.allocateId();
            if (old > 0) {
                LOG.warn("伙伴「{}」原来的编号 {} 已经被占用，改成 {}", e.getName().getString(), old, id);
            }
        }
        e.setMeta(id, e.owner(), e.ownerUuid(), e.tag());
        st = m.register(e);
        m.probes.remove(e.getUUID());
        if (!e.freshSpawn) {
            JsonObject ev = Json.event("adopted");
            ev.addProperty("companion", st.id);
            ev.add("info", m.companionJson(st, false));
            m.control.event(Json.str(ev));
            LOG.info("接管存档里的伙伴「{}」（编号 {}，{}）", st.name, st.id, e.getUUID());
        }
        return st;
    }

    private CompanionState register(ServerCompanion e) {
        CompanionState st = byUuid.get(e.getUUID());
        if (st == null) {
            st = new CompanionState(e.companionId(), e.getUUID());
            byId.put(st.id, st);
            byUuid.put(st.uuid, st);
        }
        st.owner = e.owner();
        st.ownerUuid = e.ownerUuid();
        st.tag = e.tag();
        st.remember(e);
        updateRegistry(st, e);
        snapshotDirty = true;
        return st;
    }

    static void onDied(ServerCompanion e, DamageSource source) {
        CompanionManager m = instance;
        if (m == null) {
            return;
        }
        CompanionState st = m.byUuid.get(e.getUUID());
        if (st == null) {
            return;
        }
        st.remember(e);
        if (st.task != null) {
            m.finishTask(st, st.task, "cancelled", "伙伴死了");
        }
        JsonObject ev = Json.event("died");
        ev.addProperty("companion", st.id);
        ev.addProperty("uuid", e.getUUID().toString());
        ev.addProperty("name", st.name);
        ev.addProperty("cause", source.getLocalizedDeathMessage(e).getString());
        ev.add("pos", m.posJson(st));
        m.control.event(Json.str(ev));
        LOG.info("伙伴「{}」（编号 {}）死了：{}", st.name, st.id, source.getLocalizedDeathMessage(e).getString());
    }

    /** 实体被移除（死了、被 discard、换维度、随区块卸载）。 */
    static void onRemoved(ServerCompanion e, Entity.RemovalReason reason) {
        CompanionManager m = instance;
        if (m == null) {
            return;
        }
        CompanionState st = m.byUuid.get(e.getUUID());
        if (st == null || st.entity != e) {
            return;
        }
        if (reason == Entity.RemovalReason.CHANGED_DIMENSION) {
            st.entity = null;
            if (st.task != null) {
                m.finishTask(st, st.task, "failed", "伙伴换了维度");
            }
            m.snapshotDirty = true;
            return;
        }
        if (!reason.shouldDestroy()) {
            st.entity = null; // 随区块卸载了（关服时就是这样），记录留着
            return;
        }
        String why = e.despawnRequested ? "despawn" : (reason == Entity.RemovalReason.KILLED ? "died" : "discarded");
        m.forget(st, why, e);
    }

    /** 伙伴没了：结束任务、放开强制加载的区块、删记录、发 removed。 */
    private void forget(CompanionState st, String reason, ServerCompanion e) {
        if (st.task != null) {
            finishTask(st, st.task, "cancelled", "伙伴被移除了");
        }
        byId.remove(st.id);
        byUuid.remove(st.uuid);
        RegistryEntry entry = registry.entries.remove(st.uuid);
        registry.setDirty();
        if (entry != null) {
            releaseRecorded(entry, keepChunks());
        }
        JsonObject ev = Json.event("removed");
        ev.addProperty("companion", st.id);
        ev.addProperty("uuid", st.uuid.toString());
        ev.addProperty("name", st.name);
        ev.addProperty("reason", reason);
        control.event(Json.str(ev));
        snapshotDirty = true;
        LOG.info("伙伴「{}」（编号 {}）已移除（{}）", st.name, st.id, reason);
    }

    /** attack 的计数：伙伴打死了一只。 */
    public static void onLivingDeath(LivingEntity victim, DamageSource source) {
        CompanionManager m = instance;
        if (m == null || victim instanceof CompanionEntity) {
            return;
        }
        Entity killer = source.getEntity();
        if (!(killer instanceof ServerCompanion)) {
            Entity credit = victim.getKillCredit();
            killer = credit;
        }
        if (!(killer instanceof ServerCompanion)) {
            return;
        }
        CompanionState st = m.byUuid.get(killer.getUUID());
        if (st == null || st.task == null || st.task.attack == null) {
            return;
        }
        AttackTask a = st.task.attack;
        if (a.counts(victim)) {
            a.killed++;
            m.snapshotDirty = true;
        }
    }

    /** 动作层拦下了一次拆、放（{@link ZoneInteractionManager}）。 */
    static void onZoneRefused(ServerCompanion e, BlockPos pos, Zones.Zone zone, String what) {
        CompanionManager m = instance;
        long now = System.currentTimeMillis();
        int n = e.guard().onRefused(e.controller(), pos, zone, what, now);
        if (m == null) {
            return;
        }
        CompanionState st = m.byUuid.get(e.getUUID());
        if (st != null && n >= ZoneGuard.REFUSALS_FOR_STUCK && !st.nodigReported) {
            st.nodigReported = true;
            m.stuck(st, "nodig", "伙伴反复想" + what + "禁挖区域「" + zone.name + "」里的方块（30 秒里拦下 " + n
                    + " 次），绕不过去的话请换个目标或者先让它走开");
        }
    }

    // ---------------------------------------------------------------- 每秒

    private void perSecond(long now) {
        for (CompanionState st : new ArrayList<CompanionState>(byId.values())) {
            ServerCompanion e = st.live();
            if (e == null) {
                continue;
            }
            st.remember(e);
            updateRegistry(st, e);
            PlayerEngineController c = e.controller();
            if (c != null) {
                e.guard().ensureAvoiders(c);
            }
            TaskRun t = st.task;
            if (t == null) {
                continue;
            }
            if (t.deadline() > 0 && now > t.deadline()) {
                finishTask(st, t, "timeout", "超过了 " + t.timeout + " 秒");
                if (c != null) {
                    c.stop();
                }
                continue;
            }
            if (t.player != null && server.getPlayerList().getPlayerByName(t.player) == null) {
                finishTask(st, t, "failed", "玩家 " + t.player + " 下线了");
                if (c != null) {
                    c.stop();
                }
                continue;
            }
            checkStuck(st, e, t, now);
        }
        manageChunks();
        if (!reconciled && now - startedAt > RECONCILE_AFTER_MS) {
            reconcile(now);
        }
        if (reconciled && !probes.isEmpty()) {
            finishProbes(now);
        }
        markForcedChunksDirty(server);
        snapshotDirty = true;
    }

    /**
     * 每秒、关服时：让各维度的强制加载区块记录在下次存档（自动保存、关服）时写出去，见 {@link Compat#markForcedChunksDirty}。
     * 不这样做的话，重启后伙伴脚下的区块不再强制加载，伙伴放开过的区块反而可能还强制加载着。
     */
    private static void markForcedChunksDirty(MinecraftServer server) {
        for (ServerLevel level : server.getAllLevels()) {
            Compat.markForcedChunksDirty(level);
        }
    }

    private void checkStuck(CompanionState st, ServerCompanion e, TaskRun t, long now) {
        Vec3 pos = e.position();
        int count = t.item != null ? countItem(e, t.item) : -1;
        boolean progressed = st.refPos == null || pos.distanceToSqr(st.refPos) > STUCK_MOVE_SQ
                || (count >= 0 && count != st.refCount) || (t.attack != null && t.attack.killed != st.refCount);
        if (progressed) {
            st.refPos = pos;
            st.refAt = now;
            st.refCount = t.attack != null ? t.attack.killed : count;
            st.stuckReported = false;
            if (st.nodigReported && st.refPos != null) {
                st.nodigReported = false;
            }
            return;
        }
        if (st.stuckReported || now - st.refAt < STUCK_MS) {
            return;
        }
        if ("follow".equals(t.op) || "give".equals(t.op)) {
            ServerPlayer p = t.player == null ? null : server.getPlayerList().getPlayerByName(t.player);
            if (p != null && p.level() == e.level() && p.distanceToSqr(e) <= ("give".equals(t.op) ? 9 : 36)) {
                return;
            }
        }
        if (t.attack != null && !t.attack.hasTarget()) {
            return;
        }
        st.stuckReported = true;
        Zones.Zone zone = e.guard().walkOnly() ? e.guard().lastZone() : null;
        if (zone != null) {
            // 正在「只走路」：多半是禁挖区域挡住了路，绕不过去
            st.nodigReported = true;
            stuck(st, "nodig", "伙伴 " + (now - st.refAt) / 1000 + " 秒几乎没动：路被禁挖区域「" + zone.name
                    + "」挡住了，不拆不放绕不过去");
        } else {
            stuck(st, "no_progress", "伙伴 " + (now - st.refAt) / 1000 + " 秒几乎没动");
        }
    }

    private void stuck(CompanionState st, String reason, String detail) {
        JsonObject ev = Json.event("stuck");
        ev.addProperty("companion", st.id);
        ev.add("task", st.task == null ? JsonNull.INSTANCE : new JsonPrimitive(st.task.id));
        ev.addProperty("reason", reason);
        ev.addProperty("detail", detail);
        ev.add("pos", posJson(st));
        String out = Json.str(ev);
        LOG.info("伙伴「{}」卡住了：{}", st.name, out);
        control.event(out);
    }

    // ---------------------------------------------------------------- 存档记录和强制加载的区块

    /** PlayerEngine 的 ChunkLoadingTracker 替伙伴强制加载的区块：x/16、z/16 向下取整和向上取整的四种组合。 */
    static long[] engineChunks(Entity e) {
        double x = e.getX() / 16;
        double z = e.getZ() / 16;
        Set<Long> set = new LinkedHashSet<Long>();
        set.add(ChunkPos.asLong(Mth.floor(x), Mth.floor(z)));
        set.add(ChunkPos.asLong(Mth.ceil(x), Mth.ceil(z)));
        set.add(ChunkPos.asLong(Mth.floor(x), Mth.ceil(z)));
        set.add(ChunkPos.asLong(Mth.ceil(x), Mth.floor(z)));
        long[] out = new long[set.size()];
        int i = 0;
        for (long v : set) {
            out[i++] = v;
        }
        return out;
    }

    /** 记下伙伴现在的编号、名字、位置；它现在用到的区块并进它的区块记录（放开的时候再从记录里删）。 */
    private void updateRegistry(CompanionState st, ServerCompanion e) {
        RegistryEntry entry = registry.entries.get(st.uuid);
        if (entry == null) {
            entry = new RegistryEntry(st.uuid);
            registry.entries.put(st.uuid, entry);
        }
        entry.id = st.id;
        String dim = e.level().dimension().location().toString();
        Set<Long> chunks = new LinkedHashSet<Long>();
        if (dim.equals(entry.dim)) {
            for (long v : entry.chunks) {
                chunks.add(v);
            }
        } else if (entry.dim != null && entry.chunks.length > 0) {
            // 换了维度：原来那个维度的区块记录先放开
            releaseRecorded(entry, Collections.<String, Set<Long>>emptyMap());
            entry.chunks = new long[0];
        }
        for (long v : engineChunks(e)) {
            chunks.add(v);
        }
        if (entry.update(st.name == null ? "" : st.name, dim, e.getX(), e.getY(), e.getZ(), toArray(chunks))) {
            registry.setDirty();
        }
    }

    private static long[] toArray(Set<Long> set) {
        long[] out = new long[set.size()];
        int i = 0;
        for (long v : set) {
            out[i++] = v;
        }
        return out;
    }

    /**
     * 每秒管一次强制加载的区块（不依赖 PlayerEngine 自己的记账：它的 ChunkController 在坐标正好落在区块边上时会记重，
     * 有时漏放、有时漏加）：
     * <ul>
     *   <li>活着的伙伴现在用到的区块（{@link #engineChunks}）都要是强制加载的，PlayerEngine 没加上的这里补上；</li>
     *   <li>伙伴记录里有、现在谁都用不着的区块放开（还没接管的伙伴的记录、开服时就已经强制加载的区块不动）。</li>
     * </ul>
     */
    private void manageChunks() {
        Map<String, Set<Long>> keep = keepChunks();
        for (CompanionState st : byId.values()) {
            ServerCompanion e = st.live();
            if (e == null || !(e.level() instanceof ServerLevel)) {
                continue;
            }
            ServerLevel level = (ServerLevel) e.level();
            Set<Long> forced = new HashSet<Long>(level.getForcedChunks());
            for (long v : engineChunks(e)) {
                if (!forced.contains(v)) {
                    ChunkPos cp = new ChunkPos(v);
                    level.setChunkForced(cp.x, cp.z, true);
                }
            }
            RegistryEntry entry = registry.entries.get(st.uuid);
            if (entry != null && releaseRecorded(entry, keep)) {
                registry.setDirty();
            }
        }
    }

    /** 不能放开的区块（按维度）：活着的伙伴现在用的、还没接管的伙伴记录里的、开服时就强制加载的（管理员的 /forceload）。 */
    private Map<String, Set<Long>> keepChunks() {
        Map<String, Set<Long>> keep = new HashMap<String, Set<Long>>();
        for (CompanionState st : byId.values()) {
            ServerCompanion e = st.live();
            if (e != null) {
                Set<Long> set = keep.computeIfAbsent(e.level().dimension().location().toString(), k -> new HashSet<Long>());
                for (long v : engineChunks(e)) {
                    set.add(v);
                }
            }
        }
        for (RegistryEntry en : registry.entries.values()) {
            if (!byUuid.containsKey(en.uuid) && en.dim != null) {
                Set<Long> set = keep.computeIfAbsent(en.dim, k -> new HashSet<Long>());
                for (long v : en.chunks) {
                    set.add(v);
                }
            }
        }
        for (Map.Entry<String, Set<Long>> a : adminForced.entrySet()) {
            keep.computeIfAbsent(a.getKey(), k -> new HashSet<Long>()).addAll(a.getValue());
        }
        return keep;
    }

    /** 把一条记录里不在 keep 中的区块放开，并从记录里删掉；记录变了返回 true。 */
    private boolean releaseRecorded(RegistryEntry entry, Map<String, Set<Long>> keep) {
        ServerLevel level = levelById(entry.dim);
        if (level == null || entry.chunks.length == 0) {
            return false;
        }
        Set<Long> k = keep.getOrDefault(entry.dim, Collections.<Long>emptySet());
        Set<Long> forced = new HashSet<Long>(level.getForcedChunks());
        Set<Long> left = new LinkedHashSet<Long>();
        List<String> released = new ArrayList<String>();
        for (long v : entry.chunks) {
            if (k.contains(v)) {
                left.add(v);
                continue;
            }
            if (forced.contains(v)) {
                ChunkPos cp = new ChunkPos(v);
                level.setChunkForced(cp.x, cp.z, false);
                released.add(cp.x + "," + cp.z);
            }
        }
        if (!released.isEmpty()) {
            LOG.info("放开了伙伴「{}」用过、现在用不着的 {} 个强制加载区块（{}）：{}", entry.name, released.size(), entry.dim,
                    String.join(" ", released));
        }
        if (left.size() == entry.chunks.length) {
            return false;
        }
        entry.chunks = toArray(left);
        return true;
    }

    /** 开服 30 秒后做一次：没出现的伙伴去它最后的位置找一找（临时加载那个区块，票自己会过期）。 */
    private void reconcile(long now) {
        reconciled = true;
        for (RegistryEntry entry : new ArrayList<RegistryEntry>(registry.entries.values())) {
            if (byUuid.containsKey(entry.uuid)) {
                continue;
            }
            ServerLevel level = levelById(entry.dim);
            if (level == null) {
                probes.put(entry.uuid, now - PROBE_MS); // 维度都没了，直接当找不到
                continue;
            }
            ChunkPos cp = new ChunkPos(Mth.floor(entry.x) >> 4, Mth.floor(entry.z) >> 4);
            level.getChunkSource().addRegionTicket(PROBE_TICKET, cp, 2, cp);
            probes.put(entry.uuid, now);
            LOG.info("存档里的伙伴「{}」（编号 {}）开服 30 秒还没出现，加载它最后所在的区块 {},{}（{}）找一找", entry.name, entry.id,
                    cp.x, cp.z, entry.dim);
        }
    }

    private void finishProbes(long now) {
        for (Iterator<Map.Entry<UUID, Long>> it = probes.entrySet().iterator(); it.hasNext();) {
            Map.Entry<UUID, Long> p = it.next();
            if (byUuid.containsKey(p.getKey())) {
                it.remove();
                continue;
            }
            if (now - p.getValue() < PROBE_MS) {
                continue;
            }
            it.remove();
            RegistryEntry entry = registry.entries.remove(p.getKey());
            if (entry == null) {
                continue;
            }
            registry.setDirty();
            LOG.warn("存档里的伙伴「{}」（编号 {}）找不到了（可能在没装本模组时开过服、被命令清掉了），删掉记录、放开它留下的强制加载区块",
                    entry.name, entry.id);
            releaseRecorded(entry, keepChunks());
            JsonObject ev = Json.event("removed");
            ev.addProperty("companion", entry.id);
            ev.addProperty("uuid", entry.uuid.toString());
            ev.addProperty("name", entry.name);
            ev.addProperty("reason", "lost");
            control.event(Json.str(ev));
        }
    }

    private List<RegistryEntry> unadoptedEntries() {
        List<RegistryEntry> out = new ArrayList<RegistryEntry>();
        for (RegistryEntry en : registry.entries.values()) {
            if (!byUuid.containsKey(en.uuid)) {
                out.add(en);
            }
        }
        return out;
    }

    private boolean idUsedByOther(int id, UUID self) {
        for (RegistryEntry en : registry.entries.values()) {
            if (en.id == id && !en.uuid.equals(self)) {
                return true;
            }
        }
        return false;
    }

    /** 现有伙伴（包括存档里记着、还没加载的）都没用的最小编号。 */
    private int allocateId() {
        Set<Integer> used = new HashSet<Integer>(byId.keySet());
        for (RegistryEntry en : registry.entries.values()) {
            used.add(en.id);
        }
        int id = 1;
        while (used.contains(id)) {
            id++;
        }
        return id;
    }

    private ServerLevel levelById(String dim) {
        ResourceLocation rl = Compat.tryParse(dim);
        return rl == null ? null : server.getLevel(ResourceKey.create(Registries.DIMENSION, rl));
    }

    // ---------------------------------------------------------------- 快照和 JSON

    private void refreshSnapshotIfDirty() {
        if (snapshotDirty) {
            refreshSnapshot();
        }
    }

    private void refreshSnapshot() {
        snapshotDirty = false;
        JsonArray arr = new JsonArray();
        for (CompanionState st : byId.values()) {
            arr.add(companionJson(st, false));
        }
        companionsJson = Json.str(arr);
    }

    private JsonObject posJson(CompanionState st) {
        JsonObject p = new JsonObject();
        p.addProperty("dim", st.dim);
        p.addProperty("x", Json.round(st.x));
        p.addProperty("y", Json.round(st.y));
        p.addProperty("z", Json.round(st.z));
        return p;
    }

    private JsonObject companionJson(CompanionState st, boolean withInventory) {
        ServerCompanion e = st.live();
        if (e != null) {
            st.remember(e);
        }
        JsonObject o = new JsonObject();
        o.addProperty("id", st.id);
        o.addProperty("uuid", st.uuid.toString());
        o.addProperty("name", st.name);
        o.add("owner", Json.orNull(st.owner));
        o.add("ownerUuid", Json.orNull(st.ownerUuid));
        o.add("tag", Json.orNull(st.tag));
        o.addProperty("dim", st.dim);
        o.addProperty("x", Json.round(st.x));
        o.addProperty("y", Json.round(st.y));
        o.addProperty("z", Json.round(st.z));
        o.addProperty("health", Json.round(st.health));
        o.addProperty("maxHealth", Json.round(st.maxHealth));
        PlayerEngineController c = e == null ? null : e.controller();
        o.addProperty("state", c == null ? "starting" : (st.task != null ? "busy" : "idle"));
        o.add("task", st.task == null ? JsonNull.INSTANCE : st.task.json());
        o.add("lastResult", st.lastResult == null ? JsonNull.INSTANCE : st.lastResult.deepCopy());
        String engineTask = null;
        if (c != null) {
            Task pe = c.getUserTaskChain().getCurrentTask();
            if (pe != null) {
                engineTask = pe.toString();
            }
        }
        o.add("engineTask", Json.orNull(engineTask));
        if (withInventory) {
            JsonArray inv = new JsonArray();
            if (e != null) {
                LivingEntityInventory li = e.getLivingInventory();
                for (int i = 0; i < li.main.size(); i++) {
                    addSlot(inv, i, li.main.get(i));
                }
                for (int i = 0; i < li.armor.size(); i++) {
                    addSlot(inv, 36 + i, li.armor.get(i));
                }
                addSlot(inv, 40, li.offHand.get(0));
                o.addProperty("selectedSlot", li.selectedSlot);
            }
            o.add("inventory", inv);
        }
        return o;
    }

    private static void addSlot(JsonArray inv, int slot, ItemStack s) {
        if (s.isEmpty()) {
            return;
        }
        JsonObject o = new JsonObject();
        o.addProperty("slot", slot);
        o.addProperty("item", BuiltInRegistries.ITEM.getKey(s.getItem()).toString());
        o.addProperty("count", s.getCount());
        inv.add(o);
    }

    /** 伙伴扔出了一些东西（主线程，spawnAtLocation 里调用）：正在 give 这种物品时记下扔了几个。 */
    static void noteThrown(ServerCompanion e, ItemStack stack) {
        CompanionManager m = instance;
        CompanionState st = m == null ? null : m.byUuid.get(e.getUUID());
        TaskRun t = st == null ? null : st.task;
        if (t == null || t.done || !"give".equals(t.op) || stack.isEmpty()) {
            return;
        }
        Predicate<ItemStack> match = itemMatcher(t.item);
        if (match != null && match.test(stack)) {
            t.thrown += stack.getCount();
        }
    }

    /**
     * give 没交够时的原因（中文，进 detail，跟在「交出了 N 个（要 M 个），背包里还有 K 个」后面）；看不出来时为 null。
     * have 是结束时背包里的个数。
     */
    private String giveShortfall(ServerCompanion e, TaskRun t, int have) {
        ServerPlayer p = t.player == null ? null : server.getPlayerList().getPlayerByName(t.player);
        if (p == null) {
            return t.player + " 不在线";
        }
        if (p.level() != e.level()) {
            return t.player + " 不在伙伴所在的维度";
        }
        if (t.before < t.want) { // 只有 fetch 才会走到这里（不 fetch 时不够直接回 not_enough）
            int before = Math.max(t.before, 0);
            int got = t.thrown + Math.max(have, 0) - before; // 这次弄到的：扔出去的 + 剩下的 - 原有的
            String had = before == 0 ? "原来一个都没有" : "原来只有 " + before + " 个";
            return got > 0 ? had + "，只弄到了 " + got + " 个" : had + (before == 0 ? "，也没弄到" : "，也没弄到更多");
        }
        return null;
    }

    private static String clip(String s, int max) {
        return s.length() <= max ? s : s.substring(0, max) + "…";
    }

    /** 物品的匹配：先当物品 ID（不带命名空间时补 minecraft:），不是物品就当物品标签（log → #minecraft:logs）；写法不对时为 null。 */
    static Predicate<ItemStack> itemMatcher(String item) {
        ResourceLocation rl = Compat.tryParse(Args.withNamespace(item));
        Optional<Item> it = rl == null ? Optional.<Item>empty() : BuiltInRegistries.ITEM.getOptional(rl);
        if (it.isPresent() && it.get() != Items.AIR) {
            Item target = it.get();
            return s -> s.is(target);
        }
        if (rl != null) {
            String path = rl.getPath().endsWith("s") ? rl.getPath() : rl.getPath() + "s";
            TagKey<Item> tag = TagKey.create(Registries.ITEM, Compat.rl(rl.getNamespace(), path));
            return s -> s.is(tag);
        }
        return null;
    }

    /** 背包里有多少个 item（写法见 {@link #itemMatcher}）；写法不对时为 -1。 */
    static int countItem(ServerCompanion e, String item) {
        Predicate<ItemStack> match = itemMatcher(item);
        if (match == null) {
            return -1;
        }
        LivingEntityInventory li = e.getLivingInventory();
        int n = 0;
        for (ItemStack s : li.main) {
            if (match.test(s)) {
                n += s.getCount();
            }
        }
        for (ItemStack s : li.offHand) {
            if (match.test(s)) {
                n += s.getCount();
            }
        }
        return n;
    }

    static boolean isLivingType(EntityType<?> type) {
        @SuppressWarnings("unchecked")
        EntityType<? extends LivingEntity> t = (EntityType<? extends LivingEntity>) type;
        try {
            return DefaultAttributes.hasSupplier(t);
        } catch (RuntimeException e) {
            return false;
        }
    }

    // ---------------------------------------------------------------- 参数

    private CompanionState resolve(JsonObject req) {
        JsonElement v = req.get("companion");
        if (v == null || v.isJsonNull()) {
            if (byId.isEmpty()) {
                throw new OpError("no_companion", "还没有伙伴，先 spawn");
            }
            if (byId.size() > 1) {
                throw new OpError("ambiguous", "现在有 " + byId.size() + " 个伙伴，要写 companion（编号）");
            }
            return byId.values().iterator().next();
        }
        int id;
        try {
            if (!v.isJsonPrimitive() || !v.getAsJsonPrimitive().isNumber() || v.getAsDouble() != Math.rint(v.getAsDouble())) {
                throw new NumberFormatException();
            }
            id = v.getAsInt();
        } catch (RuntimeException e) {
            throw OpError.badArgs("companion 要是伙伴的编号（整数）");
        }
        CompanionState st = byId.get(id);
        if (st == null) {
            throw new OpError("no_companion", "没有编号为 " + id + " 的伙伴");
        }
        return st;
    }

    private List<CompanionState> targets(JsonObject req) {
        if (optBool(req, "all", false)) {
            return new ArrayList<CompanionState>(byId.values());
        }
        return Collections.singletonList(resolve(req));
    }

    private ServerCompanion requireReady(CompanionState st) {
        ServerCompanion e = st.live();
        if (e == null) {
            throw new OpError("no_companion", "伙伴「" + st.name + "」现在不在世界里");
        }
        if (!e.ensureController()) {
            throw new OpError("not_ready", "伙伴还没准备好（PlayerEngine 控制器还没创建），稍后再试");
        }
        return e;
    }

    private static String optString(JsonObject req, String key) {
        JsonElement v = req.get(key);
        if (v == null || v.isJsonNull()) {
            return null;
        }
        if (!v.isJsonPrimitive() || !v.getAsJsonPrimitive().isString()) {
            throw OpError.badArgs("参数 " + key + " 要是字符串");
        }
        return v.getAsString();
    }

    private static boolean optBool(JsonObject req, String key, boolean fallback) {
        JsonElement v = req.get(key);
        if (v == null || v.isJsonNull()) {
            return fallback;
        }
        if (!v.isJsonPrimitive() || !v.getAsJsonPrimitive().isBoolean()) {
            throw OpError.badArgs("参数 " + key + " 要是 true 或 false");
        }
        return v.getAsBoolean();
    }

    private static int intArg(JsonObject req, String key, int fallback, int min, int max) {
        JsonElement v = req.get(key);
        if (v == null || v.isJsonNull()) {
            return fallback;
        }
        long n;
        try {
            if (!v.isJsonPrimitive() || !v.getAsJsonPrimitive().isNumber() || v.getAsDouble() != Math.rint(v.getAsDouble())) {
                throw new NumberFormatException();
            }
            n = v.getAsLong();
        } catch (RuntimeException e) {
            throw OpError.badArgs("参数 " + key + " 要是整数");
        }
        if (n < min || n > max) {
            throw OpError.badArgs(key + " 要在 " + min + "–" + max + " 之间");
        }
        return (int) n;
    }

    private static int coord(JsonObject req, String key) {
        if (!req.has(key)) {
            throw OpError.badArgs("缺少参数 " + key);
        }
        return intArg(req, key, 0, -Args.MAX_COORD, Args.MAX_COORD);
    }

    private static int yCoord(JsonObject req, String key) {
        return intArg(req, key, 0, -Args.MAX_Y, Args.MAX_Y);
    }

    private static int timeoutArg(JsonObject req, int fallback) {
        int t = intArg(req, "timeout", fallback, 0, Args.MAX_TIMEOUT);
        if (t != 0 && t < Args.MIN_TIMEOUT) {
            throw OpError.badArgs("timeout 最少 " + Args.MIN_TIMEOUT + " 秒（0 表示不限）");
        }
        return t;
    }

    private static int timeoutArgAllowZero(JsonObject req, int fallback) {
        return timeoutArg(req, fallback);
    }

    private static String idArg(JsonObject req, String key) {
        String raw = optString(req, key);
        if (raw == null) {
            throw OpError.badArgs("缺少参数 " + key);
        }
        String id = Args.normalizeId(raw);
        if (!Args.ITEM.matcher(id).matches() || id.length() > 128) {
            throw OpError.badArgs(key + " 只能有小写字母、数字和 _ . - : /：" + raw);
        }
        return id;
    }

    private static String itemArg(JsonObject req, String key) {
        return idArg(req, key);
    }

    private static String playerName(JsonObject req, String key) {
        String name = optString(req, key);
        if (name == null) {
            throw OpError.badArgs("缺少参数 " + key);
        }
        name = name.trim();
        if (!Args.PLAYER.matcher(name).matches()) {
            throw OpError.badArgs("玩家名不对：" + name);
        }
        return name;
    }

    /** follow / give 的玩家：写了就用它，没写就用主人；都要在线。返回服务器上的正式名字。 */
    private String onlinePlayer(JsonObject req, String key, CompanionState st) {
        String name;
        if (req.has(key) && !req.get(key).isJsonNull()) {
            name = playerName(req, key);
        } else if (st.owner != null) {
            name = st.owner;
        } else {
            throw new OpError("no_player", "没写 " + key + "，伙伴也没有主人");
        }
        ServerPlayer p = server.getPlayerList().getPlayerByName(name);
        if (p == null) {
            throw new OpError("no_player", "玩家不在线：" + name);
        }
        return p.getGameProfile().getName();
    }

    private ServerLevel dimension(String dim) {
        String d = dim == null ? "" : dim.trim().toLowerCase(java.util.Locale.ROOT);
        if (!Args.DIM.matcher(d).matches()) {
            throw OpError.badArgs("dim 要写完整的维度 ID，比如 minecraft:overworld");
        }
        ServerLevel level = levelById(d);
        if (level == null) {
            throw OpError.badArgs("没有这个维度：" + d);
        }
        return level;
    }

    private static double surface(Level level, double x, double z) {
        return level.getHeight(Heightmap.Types.MOTION_BLOCKING_NO_LEAVES, Mth.floor(x), Mth.floor(z));
    }

    private static String fmt(double v) {
        return String.valueOf(Json.round(v));
    }

    /** 测试和排错用：协议常量（让 Protocol 类在加载器里被引用到，方便 jdeps 检查）。 */
    static int protocolVersion() {
        return Protocol.VERSION;
    }
}
