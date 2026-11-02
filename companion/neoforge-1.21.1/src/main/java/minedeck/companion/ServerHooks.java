package minedeck.companion;

import net.neoforged.bus.api.IEventBus;
import net.neoforged.neoforge.common.NeoForge;
import net.neoforged.neoforge.event.entity.living.LivingDeathEvent;
import net.neoforged.neoforge.event.server.ServerStartedEvent;
import net.neoforged.neoforge.event.server.ServerStoppedEvent;
import net.neoforged.neoforge.event.server.ServerStoppingEvent;
import net.neoforged.neoforge.event.tick.ServerTickEvent;

/**
 * 只在 PlayerEngine 在的时候加载（CompanionMod 里判断）：接上服务端的实体工厂和事件。
 * 这个类引用了挂着 PlayerEngine 的类，客户端没装 PlayerEngine 时从不加载它。
 */
final class ServerHooks {

    private ServerHooks() {
    }

    static void register(IEventBus modBus) {
        CompanionMod.serverFactory = ServerCompanion::new;
        NeoForge.EVENT_BUS.addListener(ServerHooks::onServerStarted);
        NeoForge.EVENT_BUS.addListener(ServerHooks::onServerStopping);
        NeoForge.EVENT_BUS.addListener(ServerHooks::onServerStopped);
        NeoForge.EVENT_BUS.addListener(ServerHooks::onServerTick);
        NeoForge.EVENT_BUS.addListener(ServerHooks::onLivingDeath);
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

    private static void onServerTick(ServerTickEvent.Post event) {
        CompanionManager.tick();
    }

    private static void onLivingDeath(LivingDeathEvent event) {
        if (!event.getEntity().level().isClientSide()) {
            CompanionManager.onLivingDeath(event.getEntity(), event.getSource());
        }
    }
}
