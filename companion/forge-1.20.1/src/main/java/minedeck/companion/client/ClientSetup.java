package minedeck.companion.client;

import minedeck.companion.CompanionMod;
import net.minecraft.client.renderer.entity.NoopRenderer;
import net.minecraftforge.client.event.EntityRenderersEvent;
import net.minecraftforge.eventbus.api.IEventBus;

/** 客户端才加载（CompanionMod 按 FMLEnvironment.dist 判断）：给伙伴实体注册渲染器。专用服务器上不加载这个类。 */
public final class ClientSetup {

    private ClientSetup() {
    }

    public static void init(IEventBus modBus) {
        modBus.addListener(ClientSetup::onRegisterRenderers);
    }

    private static void onRegisterRenderers(EntityRenderersEvent.RegisterRenderers event) {
        event.registerEntityRenderer(CompanionMod.COMPANION.get(), CompanionRenderer::new);
        if (CompanionMod.placeholderType() != null) {
            event.registerEntityRenderer(CompanionMod.placeholderType(), NoopRenderer::new);
        }
    }
}
