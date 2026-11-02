package minedeck.voicebridge;

import de.maxhenkel.voicechat.api.ForgeVoicechatPlugin;
import de.maxhenkel.voicechat.api.VoicechatApi;
import de.maxhenkel.voicechat.api.VoicechatConnection;
import de.maxhenkel.voicechat.api.VoicechatPlugin;
import de.maxhenkel.voicechat.api.VoicechatServerApi;
import de.maxhenkel.voicechat.api.events.EventRegistration;
import de.maxhenkel.voicechat.api.events.PlayerConnectedEvent;
import de.maxhenkel.voicechat.api.events.PlayerDisconnectedEvent;
import de.maxhenkel.voicechat.api.events.VoicechatServerStartedEvent;
import de.maxhenkel.voicechat.api.events.VoicechatServerStoppedEvent;

import java.util.UUID;

/**
 * SVC 插件入口。
 *
 * <p>Forge / NeoForge 上的 SVC 会扫描所有模组文件里带 {@link ForgeVoicechatPlugin} 注解的类，
 * 用公开的无参构造函数创建实例。这个类只引用 {@code java.*} 和 SVC API，不引用任何 Minecraft
 * 或加载器的类，所以同一个 jar 能在两个加载器上用。
 *
 * <p>每次 SVC 的语音服务器启动（包括重启）都新建一个 {@link VoiceBridge}，停止时整个丢掉。
 */
@ForgeVoicechatPlugin
public final class BridgePlugin implements VoicechatPlugin {

    public static final String PLUGIN_ID = "minedeck_voicebridge";
    public static final String VERSION = "0.2.0";

    private VoiceBridge current;

    public BridgePlugin() {
    }

    @Override
    public String getPluginId() {
        return PLUGIN_ID;
    }

    @Override
    public void initialize(VoicechatApi api) {
        Log.info("MineDeck 音乐共享桥接 " + VERSION + " 已被 SVC 加载，等待语音服务器启动");
    }

    @Override
    public void registerEvents(EventRegistration registration) {
        registration.registerEvent(VoicechatServerStartedEvent.class, event -> start(event.getVoicechat()));
        registration.registerEvent(VoicechatServerStoppedEvent.class, event -> stop("语音服务器已停止"));
        registration.registerEvent(PlayerConnectedEvent.class, event -> playerConnected(event.getConnection()));
        registration.registerEvent(PlayerDisconnectedEvent.class, event -> playerDisconnected(event.getPlayerUuid()));
    }

    private synchronized void start(VoicechatServerApi api) {
        try {
            if (current != null) {
                current.shutdown("语音服务器重启");
                current = null;
            }
            VoiceBridge bridge = new VoiceBridge(api);
            current = bridge;
            bridge.start();
        } catch (Throwable t) {
            Log.error("启动音乐共享失败", t);
        }
    }

    private synchronized void stop(String reason) {
        try {
            if (current != null) {
                current.shutdown(reason);
            }
        } catch (Throwable t) {
            Log.error("停止音乐共享时出错", t);
        } finally {
            current = null;
        }
    }

    private void playerConnected(VoicechatConnection connection) {
        VoiceBridge bridge;
        synchronized (this) {
            bridge = current;
        }
        if (bridge != null && connection != null) {
            try {
                bridge.playerConnected(connection);
            } catch (Throwable t) {
                Log.error("处理玩家连上语音时出错", t);
            }
        }
    }

    private void playerDisconnected(UUID playerUuid) {
        VoiceBridge bridge;
        synchronized (this) {
            bridge = current;
        }
        if (bridge != null && playerUuid != null) {
            try {
                bridge.playerDisconnected(playerUuid);
            } catch (Throwable t) {
                Log.error("处理玩家断开语音时出错", t);
            }
        }
    }
}
