package minedeck.voicebridge.fallback;

/**
 * 备用方案（javafml 版 jar）里给 Forge 用的空入口类。只有 Forge 认 {@code net.minecraftforge.fml.common.Mod}
 * 这个注解；NeoForge 扫描时不认识它，也不会加载这个类。真正干活的还是 SVC 插件类。
 */
@net.minecraftforge.fml.common.Mod("minedeck_voicebridge")
public final class ForgeEntry {
    public ForgeEntry() {
    }
}
