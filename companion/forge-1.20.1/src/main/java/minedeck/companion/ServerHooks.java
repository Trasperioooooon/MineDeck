package minedeck.companion;

import net.minecraftforge.common.MinecraftForge;
import net.minecraftforge.event.TickEvent;
import net.minecraftforge.event.entity.living.LivingDeathEvent;
import net.minecraftforge.event.server.ServerStartedEvent;
import net.minecraftforge.event.server.ServerStoppedEvent;
import net.minecraftforge.event.server.ServerStoppingEvent;
import net.minecraftforge.eventbus.api.IEventBus;

/**
 * 只在 PlayerEngine 在的时候加载（CompanionMod 里判断）：接上服务端的实体工厂和事件。
 * 这个类引用了挂着 PlayerEngine 的类，客户端没装 PlayerEngine 时从不加载它。
 */
final class ServerHooks {

    private ServerHooks() {
    }

    static void register(IEventBus modBus) {
        CompanionMod.serverFactory = ServerCompanion::new;
        MinecraftForge.EVENT_BUS.addListener(ServerHooks::onServerStarted);
        MinecraftForge.EVENT_BUS.addListener(ServerHooks::onServerStopping);
        MinecraftForge.EVENT_BUS.addListener(ServerHooks::onServerStopped);
        MinecraftForge.EVENT_BUS.addListener(ServerHooks::onServerTick);
        MinecraftForge.EVENT_BUS.addListener(ServerHooks::onLivingDeath);
    }

    private static void onServerStarted(ServerStartedEvent event) {
        CompanionManager.start(event.getServer(), CompanionMod.version);
    }

    private static void onServerStopping(ServerStoppingEvent event) {
        CompanionManager.stop();
    }

    private static void onServerStopped(ServerStoppedEvent event) {
        CompanionManager.stopped();
    }

    private static void onServerTick(TickEvent.ServerTickEvent event) {
        if (event.phase == TickEvent.Phase.END) {
            CompanionManager.tick();
        }
    }

    private static void onLivingDeath(LivingDeathEvent event) {
        if (!event.getEntity().level().isClientSide()) {
            CompanionManager.onLivingDeath(event.getEntity(), event.getSource());
        }
    }
}
