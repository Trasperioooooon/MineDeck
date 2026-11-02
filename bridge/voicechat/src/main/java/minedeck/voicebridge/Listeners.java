package minedeck.voicebridge;

import de.maxhenkel.voicechat.api.VoicechatConnection;
import de.maxhenkel.voicechat.api.VoicechatServerApi;
import de.maxhenkel.voicechat.api.audiochannel.StaticAudioChannel;

import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;

/**
 * 听众：已经连上 SVC 语音的玩家，去掉配置里排除的人。
 *
 * <p>两种推送方式，启动时按 SVC 实际提供的 API 自动选：
 * <ul>
 *   <li>多目标（SVC 2.6.0 起）：一个静态音频通道，{@code addTarget} / {@code removeTarget} 增减听众。</li>
 *   <li>兼容旧版：每位听众一个静态音频通道（旧接口，2.6.0 起标为过时，但一直能用）。</li>
 * </ul>
 * 两种方式都是同一份 Opus 数据发给所有人，只编码一次。
 */
final class Listeners {

    static final String CATEGORY_ID = "minedeck_music";
    static final UUID CHANNEL_ID = UUID.nameUUIDFromBytes("minedeck_voicebridge:music".getBytes(StandardCharsets.UTF_8));

    private final VoicechatServerApi api;
    final boolean multiTarget;
    final boolean canBypassIsolation;

    private StaticAudioChannel shared;
    private boolean sharedFailed;
    private final Map<UUID, Player> players = new ConcurrentHashMap<UUID, Player>();
    private final Map<UUID, StaticAudioChannel> legacyChannels = new ConcurrentHashMap<UUID, StaticAudioChannel>();
    private final Set<UUID> targets = ConcurrentHashMap.<UUID>newKeySet();
    private BridgeConfig config = BridgeConfig.disabled();

    private static final class Player {
        final UUID id;
        final VoicechatConnection connection;
        final String name;

        Player(UUID id, VoicechatConnection connection, String name) {
            this.id = id;
            this.connection = connection;
            this.name = name;
        }

        String label() {
            return name != null ? name + "（" + id + "）" : id.toString();
        }
    }

    Listeners(VoicechatServerApi api) {
        this.api = api;
        this.multiTarget = hasMethod(VoicechatServerApi.class, "createStaticAudioChannel", UUID.class)
                && hasMethod(StaticAudioChannel.class, "addTarget", VoicechatConnection.class)
                && hasMethod(StaticAudioChannel.class, "removeTarget", VoicechatConnection.class);
        this.canBypassIsolation = hasMethod(StaticAudioChannel.class, "setBypassGroupIsolation", boolean.class);
    }

    String modeDescription() {
        return multiTarget ? "多目标静态通道（SVC 2.6 及以上）" : "每位听众一个静态通道（旧版 SVC 兼容模式）";
    }

    /** 多目标模式下建好共用的通道。 */
    synchronized void open() {
        if (!multiTarget) {
            return;
        }
        try {
            shared = api.createStaticAudioChannel(CHANNEL_ID);
        } catch (Throwable t) {
            Log.error("创建静态音频通道失败", t);
            shared = null;
        }
        if (shared == null) {
            sharedFailed = true;
            Log.warn("SVC 没有返回静态音频通道（语音服务器可能没在运行），音乐推送不可用");
            return;
        }
        shared.setCategory(CATEGORY_ID);
        applyBypass(shared);
    }

    synchronized void configure(BridgeConfig newConfig) {
        boolean bypassChanged = newConfig.bypassGroupIsolation != config.bypassGroupIsolation;
        config = newConfig;
        if (bypassChanged) {
            if (shared != null) {
                applyBypass(shared);
            }
            for (StaticAudioChannel c : legacyChannels.values()) {
                applyBypass(c);
            }
        }
        for (Player p : players.values()) {
            refresh(p);
        }
    }

    synchronized void connected(VoicechatConnection connection) {
        if (connection.getPlayer() == null) {
            return;
        }
        UUID id = connection.getPlayer().getUuid();
        String name = PlayerNames.nameOf(connection.getPlayer().getPlayer());
        Player old = players.get(id);
        if (old != null && name == null) {
            name = old.name;
        }
        Player p = new Player(id, connection, name);
        players.put(id, p);
        if (old != null) {
            // 重连：先按旧连接拿掉，再按新连接加回来
            removeTarget(old);
        }
        boolean added = refresh(p);
        String note = "";
        if (!added) {
            note = config.excludes(p.id, p.name) ? "（在排除名单里，不推送）" : "（推送通道不可用，见上面的错误）";
        }
        Log.info("玩家连上语音：" + p.label() + note + "，当前听众 " + targets.size() + " 位");
    }

    synchronized void disconnected(UUID id) {
        Player p = players.remove(id);
        if (p == null) {
            return;
        }
        removeTarget(p);
        Log.info("玩家断开语音：" + p.label() + "，当前听众 " + targets.size() + " 位");
    }

    int count() {
        return targets.size();
    }

    /** 引擎线程调用：把一帧 Opus 发给所有听众。 */
    void send(byte[] opus) {
        if (targets.isEmpty()) {
            return;
        }
        if (multiTarget) {
            StaticAudioChannel c = shared;
            if (c != null && !c.isClosed()) {
                c.send(opus);
            }
            return;
        }
        for (StaticAudioChannel c : legacyChannels.values()) {
            if (!c.isClosed()) {
                c.send(opus);
            }
        }
    }

    /** 引擎线程调用：一段声音结束，让客户端收尾（SVC 发一个空包）。 */
    void flush() {
        if (multiTarget) {
            StaticAudioChannel c = shared;
            if (c != null && !c.isClosed()) {
                c.flush();
            }
            return;
        }
        for (StaticAudioChannel c : legacyChannels.values()) {
            if (!c.isClosed()) {
                c.flush();
            }
        }
    }

    synchronized void close() {
        if (shared != null) {
            try {
                if (!shared.isClosed()) {
                    shared.clearTargets();
                }
            } catch (Throwable ignored) {
                // 语音服务器已经关了
            }
        }
        shared = null;
        legacyChannels.clear();
        targets.clear();
        players.clear();
    }

    /** 按当前配置决定这位玩家是不是听众。返回是不是听众。 */
    private boolean refresh(Player p) {
        boolean want = !config.excludes(p.id, p.name);
        boolean has = targets.contains(p.id);
        if (want && !has) {
            addTarget(p);
        } else if (!want && has) {
            removeTarget(p);
        }
        return targets.contains(p.id);
    }

    private void addTarget(Player p) {
        if (multiTarget) {
            if (shared == null) {
                if (!sharedFailed) {
                    open();
                }
                if (shared == null) {
                    return;
                }
            }
            shared.addTarget(p.connection);
            targets.add(p.id);
            return;
        }
        StaticAudioChannel c = createLegacyChannel(p);
        if (c != null) {
            legacyChannels.put(p.id, c);
            targets.add(p.id);
        }
    }

    private void removeTarget(Player p) {
        if (!targets.remove(p.id)) {
            return;
        }
        if (multiTarget) {
            if (shared != null) {
                shared.removeTarget(p.connection);
            }
        } else {
            legacyChannels.remove(p.id);
        }
    }

    @SuppressWarnings("deprecation")
    private StaticAudioChannel createLegacyChannel(Player p) {
        try {
            UUID channelId = UUID.nameUUIDFromBytes(("minedeck_voicebridge:" + p.id).getBytes(StandardCharsets.UTF_8));
            StaticAudioChannel c = api.createStaticAudioChannel(channelId, p.connection.getPlayer().getServerLevel(), p.connection);
            if (c == null) {
                Log.warn("SVC 没有为 " + p.label() + " 建立静态音频通道");
                return null;
            }
            c.setCategory(CATEGORY_ID);
            applyBypass(c);
            return c;
        } catch (Throwable t) {
            Log.error("为 " + p.label() + " 建立静态音频通道失败", t);
            return null;
        }
    }

    private void applyBypass(StaticAudioChannel c) {
        if (canBypassIsolation) {
            c.setBypassGroupIsolation(config.bypassGroupIsolation);
        }
    }

    static boolean hasMethod(Class<?> type, String name, Class<?>... parameters) {
        try {
            type.getMethod(name, parameters);
            return true;
        } catch (NoSuchMethodException e) {
            return false;
        } catch (Throwable t) {
            return false;
        }
    }

    /**
     * 从 SVC 给的玩家对象（就是 Minecraft 的 ServerPlayer）里取名字，只用反射，不引用 Minecraft 的类：
     * 找一个公开、无参、返回 {@code com.mojang.authlib.GameProfile} 的方法，再调它的 {@code getName()}。
     * Forge 1.20.1 运行时的方法名是混淆过的（m_36316_），NeoForge 是 getGameProfile，所以按返回类型找，不按名字找。
     * 拿不到就返回 null，这时只能按 UUID 排除。
     */
    static final class PlayerNames {
        private PlayerNames() {
        }

        static String nameOf(Object player) {
            if (player == null) {
                return null;
            }
            try {
                for (Method m : player.getClass().getMethods()) {
                    if (m.getParameterTypes().length != 0 || Modifier.isStatic(m.getModifiers())) {
                        continue;
                    }
                    if (!"com.mojang.authlib.GameProfile".equals(m.getReturnType().getName())) {
                        continue;
                    }
                    Object profile = m.invoke(player);
                    if (profile == null) {
                        continue;
                    }
                    for (String getter : new String[]{"getName", "name"}) {
                        try {
                            Object name = profile.getClass().getMethod(getter).invoke(profile);
                            if (name instanceof String && !((String) name).isEmpty()) {
                                return (String) name;
                            }
                        } catch (NoSuchMethodException ignored) {
                            // 换下一个方法名
                        }
                    }
                }
            } catch (Throwable ignored) {
                // 拿不到名字不影响推送
            }
            return null;
        }
    }
}
