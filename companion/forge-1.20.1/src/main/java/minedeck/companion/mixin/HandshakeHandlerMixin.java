package minedeck.companion.mixin;

import java.util.function.Supplier;

import minedeck.companion.JoinCheck;
import net.minecraftforge.network.HandshakeHandler;
import net.minecraftforge.network.HandshakeMessages;
import net.minecraftforge.network.NetworkEvent;
import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.injection.At;
import org.spongepowered.asm.mixin.injection.Inject;
import org.spongepowered.asm.mixin.injection.callback.CallbackInfo;

/**
 * 服务器收到 Forge 客户端的模组清单时（登录阶段、同步注册表之前），先看它有没有伙伴模组的通道，没有就用中文拒掉
 * （{@link JoinCheck#rejectLogin}）。否则客户端会在同步注册表时因为不认识伙伴的实体类型被 Forge 拒掉，只看到英文提示。
 * 只是在 Forge 的处理前面加一个检查；找不到注入点时（以后 Forge 改了这里）mixin 不生效，退回 Forge 自己的提示。
 */
@Mixin(value = HandshakeHandler.class, remap = false)
public abstract class HandshakeHandlerMixin {

    @Inject(method = "handleClientModListOnServer", at = @At("HEAD"), cancellable = true, require = 0, remap = false)
    private void minedeck_companion$checkMod(HandshakeMessages.C2SModListReply reply, Supplier<NetworkEvent.Context> ctx,
                                             CallbackInfo ci) {
        if (!JoinCheck.hasMod(reply.getChannels())) {
            ctx.get().setPacketHandled(true);
            JoinCheck.rejectLogin(ctx.get().getNetworkManager());
            ci.cancel();
        }
    }
}
