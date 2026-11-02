package minedeck.companion;

import java.util.function.Consumer;

import io.netty.buffer.ByteBuf;
import net.minecraft.ChatFormatting;
import net.minecraft.network.chat.Component;
import net.minecraft.network.codec.StreamCodec;
import net.minecraft.network.protocol.common.custom.CustomPacketPayload;
import net.minecraft.network.protocol.configuration.ServerConfigurationPacketListener;
import net.minecraft.server.network.ConfigurationTask;
import net.neoforged.neoforge.network.configuration.ICustomConfigurationTask;
import net.neoforged.neoforge.network.event.RegisterConfigurationTasksEvent;
import net.neoforged.neoforge.network.event.RegisterPayloadHandlersEvent;

/**
 * 没装伙伴模组的客户端，进服时就干净地拒掉，并用中文告诉对方要装什么。
 *
 * <p>做法：本模组注册一个可选的网络通道 {@code minedeck_companion:hello}（客户端装了本模组就有）。进服的配置阶段里，
 * 一个配置任务看客户端有没有这个通道，没有就断开并给出提示。
 *
 * <ul>
 *   <li>原版客户端：NeoForge 不给它同步模组的注册表，任务照常跑到这里，收到中文提示。</li>
 *   <li>装了 NeoForge、没装本模组的客户端：NeoForge 在配置阶段一开始就同步注册表，缺本模组的实体类型会先被 NeoForge
 *       拒掉（英文的「未知的注册表键」）。为了让它也看到中文提示，{@code ConfigurationInitializationMixin} 把这个任务插到
 *       注册表同步的前面；mixin 没生效时（以后 NeoForge 改了内部代码）退回 NeoForge 自己的提示。</li>
 * </ul>
 * 通道版本固定为 "1"：伙伴模组的客户端部分只有实体外形，不同版本之间互相兼容，不按模组版本卡人。
 */
public final class JoinCheck {

    static final String CHANNEL_VERSION = "1";

    public record Hello() implements CustomPacketPayload {
        public static final Type<Hello> TYPE = new Type<Hello>(Compat.rl(CompanionMod.MODID, "hello"));
        public static final StreamCodec<ByteBuf, Hello> CODEC = StreamCodec.unit(new Hello());

        @Override
        public Type<? extends CustomPacketPayload> type() {
            return TYPE;
        }
    }

    private JoinCheck() {
    }

    static void onRegisterPayloads(RegisterPayloadHandlersEvent event) {
        event.registrar(CHANNEL_VERSION).optional().configurationToClient(Hello.TYPE, Hello.CODEC, (payload, ctx) -> {
        });
    }

    static void onRegisterTasks(RegisterConfigurationTasksEvent event) {
        event.register(new Task(event.getListener()));
    }

    /** 拒绝时给玩家看的话。 */
    static Component message() {
        String jar = "minedeck-companion-neoforge-1.21.1-" + CompanionMod.version + ".jar";
        return Component.literal("这个服务器开着 MineDeck 的 AI 伙伴，进服前要先在客户端装伙伴模组：\n\n")
                .append(Component.literal("MineDeck Companion " + CompanionMod.version + "（NeoForge 1.21.1）\n")
                        .withStyle(ChatFormatting.AQUA))
                .append(Component.literal(jar + "\n\n").withStyle(ChatFormatting.GRAY))
                .append(Component.literal("客户端只要装这一个（PlayerEngine 和 Architectury API 只装在服务器上）。\n"
                        + "文件可以向开服的人要。装好后重启游戏，再进服。"));
    }

    /** 配置阶段的任务：客户端有本模组的通道就放行，没有就断开。 */
    public record Task(ServerConfigurationPacketListener listener) implements ICustomConfigurationTask {
        public static final ConfigurationTask.Type TYPE = new ConfigurationTask.Type(CompanionMod.MODID + ":join_check");

        @Override
        public void run(Consumer<CustomPacketPayload> sender) {
            if (listener.hasChannel(Hello.TYPE)) {
                listener.finishCurrentTask(TYPE);
                return;
            }
            CompanionMod.LOG.info("拒绝没装伙伴模组的客户端进服：{}（{}）", listener.getConnection().getRemoteAddress(),
                    listener.getConnectionType());
            listener.disconnect(message());
        }

        @Override
        public ConfigurationTask.Type type() {
            return TYPE;
        }
    }
}
