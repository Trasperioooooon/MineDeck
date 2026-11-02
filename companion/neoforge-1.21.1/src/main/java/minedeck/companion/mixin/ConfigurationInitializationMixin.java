package minedeck.companion.mixin;

import java.util.function.Consumer;

import minedeck.companion.JoinCheck;
import net.minecraft.network.protocol.configuration.ServerConfigurationPacketListener;
import net.minecraft.server.network.ConfigurationTask;
import net.neoforged.neoforge.network.ConfigurationInitialization;
import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.injection.At;
import org.spongepowered.asm.mixin.injection.Inject;
import org.spongepowered.asm.mixin.injection.callback.CallbackInfo;

/**
 * 把「客户端装没装伙伴模组」的检查（{@link JoinCheck.Task}）插到 NeoForge 同步注册表之前：装了 NeoForge、没装本模组的
 * 客户端，否则会先因为不认识本模组的实体类型被 NeoForge 拒掉，看不到中文提示。只是多加一个配置任务，不改 NeoForge 的逻辑；
 * 找不到注入点时（以后 NeoForge 改了这里）mixin 不生效，退回 NeoForge 自己的提示，原版客户端照样由 JoinCheck 拒掉。
 */
@Mixin(value = ConfigurationInitialization.class, remap = false)
public abstract class ConfigurationInitializationMixin {

    @Inject(method = "configureEarlyTasks", at = @At("HEAD"), require = 0, remap = false)
    private static void minedeck_companion$joinCheckFirst(ServerConfigurationPacketListener listener,
                                                          Consumer<ConfigurationTask> tasks, CallbackInfo ci) {
        tasks.accept(new JoinCheck.Task(listener));
    }
}
