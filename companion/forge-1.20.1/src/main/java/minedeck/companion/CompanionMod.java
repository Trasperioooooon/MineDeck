package minedeck.companion;

import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.MobCategory;
import net.minecraft.world.level.Level;
import net.minecraftforge.common.MinecraftForge;
import net.minecraftforge.event.entity.EntityAttributeCreationEvent;
import net.minecraftforge.eventbus.api.IEventBus;
import net.minecraftforge.fml.ModList;
import net.minecraftforge.fml.ModLoadingContext;
import net.minecraftforge.fml.common.Mod;
import net.minecraftforge.fml.javafmlmod.FMLJavaModLoadingContext;
import net.minecraftforge.fml.loading.FMLEnvironment;
import net.minecraftforge.registries.DeferredRegister;
import net.minecraftforge.registries.ForgeRegistries;
import net.minecraftforge.registries.RegistryObject;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * MineDeck 伙伴模组的入口（Forge 1.20.1），做法和 NeoForge 1.21.1 版一样。
 *
 * <p>客户端和服务端分开：这个类、{@link CompanionEntity}、client 包、{@link JoinCheck}、{@link PlaceholderEntity} 两边都加载，
 * 从不引用 PlayerEngine 的类；挂着 PlayerEngine 的部分只在 PlayerEngine 在的时候由 {@link ServerHooks} 接上。所以玩家的
 * 客户端只装本模组就行（PlayerEngine 和 Architectury API 在 mods.toml 里只要求服务端装）。不登录 Player2、不调用 Player2 的
 * 网页接口（伙伴在 PlayerEngine 那边没有主人，它的对话层不处理）。
 */
@Mod(CompanionMod.MODID)
public final class CompanionMod {

    public static final String MODID = "minedeck_companion";
    public static final Logger LOG = LoggerFactory.getLogger(MODID);

    public static final DeferredRegister<EntityType<?>> ENTITIES = DeferredRegister.create(ForgeRegistries.ENTITY_TYPES, MODID);
    public static final RegistryObject<EntityType<CompanionEntity>> COMPANION = ENTITIES.register("companion",
            () -> EntityType.Builder.<CompanionEntity>of(CompanionMod::create, MobCategory.MISC)
                    .sized(0.6F, 1.8F)
                    .clientTrackingRange(10)
                    .updateInterval(2)
                    .build(MODID + ":companion"));

    static String version = "0.0.0";
    /** 服务端的实体工厂（PlayerEngine 在时由 ServerHooks 填上）。 */
    static volatile EntityType.EntityFactory<CompanionEntity> serverFactory;
    /** 有没有注册 PlayerEngine 的占位实体类型（没装 PlayerEngine 时）。 */
    static boolean placeholder;

    @SuppressWarnings("removal")
    public CompanionMod() {
        IEventBus modBus = FMLJavaModLoadingContext.get().getModEventBus();
        version = ModLoadingContext.get().getActiveContainer().getModInfo().getVersion().toString();
        ENTITIES.register(modBus);
        modBus.addListener(CompanionMod::onAttributes);
        JoinCheck.registerChannel();
        MinecraftForge.EVENT_BUS.addListener(JoinCheck::onPlayerLoggedIn);
        boolean engine = ModList.get().isLoaded("playerengine");
        if (engine) {
            ServerHooks.register(modBus);
        } else {
            placeholder = true;
            PlaceholderEntity.register(modBus);
        }
        if (FMLEnvironment.dist.isClient()) {
            minedeck.companion.client.ClientSetup.init(modBus);
        }
        LOG.info("MineDeck 伙伴模组 {} 已加载（{}）", version,
                engine ? "带 PlayerEngine：能刷出、指挥伙伴" : "没有 PlayerEngine：只负责显示伙伴，注册了 PlayerEngine 实体类型的占位");
    }

    /** 实体类型的工厂：服务端（有 PlayerEngine 时）造挂着 PlayerEngine 的 ServerCompanion，客户端造只有外形的 CompanionEntity。 */
    static CompanionEntity create(EntityType<CompanionEntity> type, Level level) {
        EntityType.EntityFactory<CompanionEntity> f = serverFactory;
        if (f != null && !level.isClientSide()) {
            return f.create(type, level);
        }
        return new CompanionEntity(type, level);
    }

    /** PlayerEngine 占位实体的类型；没注册（装了 PlayerEngine）时为 null。 */
    public static EntityType<PlaceholderEntity> placeholderType() {
        return placeholder && PlaceholderEntity.TYPE != null ? PlaceholderEntity.TYPE.get() : null;
    }

    private static void onAttributes(EntityAttributeCreationEvent event) {
        event.put(COMPANION.get(), CompanionEntity.createAttributes().build());
    }
}
