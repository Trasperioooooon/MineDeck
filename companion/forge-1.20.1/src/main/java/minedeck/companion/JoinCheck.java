package minedeck.companion;

import java.util.Map;

import net.minecraft.ChatFormatting;
import net.minecraft.network.Connection;
import net.minecraft.network.chat.Component;
import net.minecraft.resources.ResourceLocation;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.network.ServerLoginPacketListenerImpl;
import net.minecraftforge.event.entity.player.PlayerEvent;
import net.minecraftforge.network.ConnectionData;
import net.minecraftforge.network.NetworkHooks;
import net.minecraftforge.network.NetworkRegistry;

/**
 * 没装伙伴模组的客户端，进服时就干净地拒掉，并用中文告诉对方要装什么。
 *
 * <p>做法：本模组注册一个网络通道 {@code minedeck_companion:hello}（客户端装了本模组就有），两边都接受「没有」，所以
 * Forge 自己的通道检查不会因为它拒人。
 * <ul>
 *   <li>装了 Forge、没装本模组的客户端：Forge 在登录阶段先交换模组清单、再同步注册表，缺本模组的实体类型会被 Forge 拒掉
 *       （英文的提示）。{@code HandshakeHandlerMixin} 在服务器收到客户端的模组清单时先看有没有这个通道，没有就用中文拒掉；
 *       mixin 没生效时（以后 Forge 改了内部代码）退回 Forge 自己的提示。</li>
 *   <li>原版客户端：Forge 不和它交换模组清单，进服那一刻（PlayerLoggedInEvent）拒掉。原版 1.20.1 客户端把认不出的实体
 *       画成猪，不会因为提前看到伙伴而出错。</li>
 * </ul>
 * 通道版本固定为 "1"：伙伴模组的客户端部分只有实体外形，不同版本之间互相兼容，不按模组版本卡人。
 */
public final class JoinCheck {

    static final String CHANNEL_VERSION = "1";
    static final ResourceLocation CHANNEL = Compat.rl(CompanionMod.MODID, "hello");

    private JoinCheck() {
    }

    /** 模组构造时注册通道（两边都要有）。 */
    static void registerChannel() {
        NetworkRegistry.newEventChannel(CHANNEL, () -> CHANNEL_VERSION, v -> true, v -> true);
    }

    /** 拒绝时给玩家看的话。 */
    static Component message() {
        String jar = "minedeck-companion-forge-1.20.1-" + CompanionMod.version + ".jar";
        return Component.literal("这个服务器开着 MineDeck 的 AI 伙伴，进服前要先在客户端装伙伴模组：\n\n")
                .append(Component.literal("MineDeck Companion " + CompanionMod.version + "（Forge 1.20.1）\n")
                        .withStyle(ChatFormatting.AQUA))
                .append(Component.literal(jar + "\n\n").withStyle(ChatFormatting.GRAY))
                .append(Component.literal("客户端只要装这一个（PlayerEngine 和 Architectury API 只装在服务器上）。\n"
                        + "文件可以向开服的人要。装好后重启游戏，再进服。"));
    }

    /** Forge 客户端交来的通道清单里有没有本模组的通道。 */
    public static boolean hasMod(Map<ResourceLocation, String> channels) {
        return channels != null && channels.containsKey(CHANNEL);
    }

    /** 登录阶段拒掉（HandshakeHandlerMixin 调用）：发登录阶段的断开包，客户端能看到这段话。 */
    public static void rejectLogin(Connection connection) {
        CompanionMod.LOG.info("拒绝没装伙伴模组的 Forge 客户端进服：{}", connection.getRemoteAddress());
        Component msg = message();
        if (connection.getPacketListener() instanceof ServerLoginPacketListenerImpl login) {
            login.disconnect(msg);
        } else {
            connection.disconnect(msg);
        }
    }

    /** 进服那一刻：原版客户端（或者 mixin 没生效时漏过来的 Forge 客户端）没有本模组的通道，拒掉。 */
    static void onPlayerLoggedIn(PlayerEvent.PlayerLoggedInEvent event) {
        if (!(event.getEntity() instanceof ServerPlayer player)) {
            return;
        }
        Connection connection = player.connection.connection;
        if (connection.isMemoryConnection()) {
            return; // 单人游戏
        }
        ConnectionData data = NetworkHooks.getConnectionData(connection);
        boolean ok = !NetworkHooks.isVanillaConnection(connection) && data != null && hasMod(data.getChannels());
        if (!ok) {
            CompanionMod.LOG.info("拒绝没装伙伴模组的客户端进服：{}（{}）", player.getGameProfile().getName(),
                    NetworkHooks.isVanillaConnection(connection) ? "原版客户端" : "Forge 客户端");
            player.connection.disconnect(message());
        }
    }
}
