package minedeck.companion;

import net.minecraft.core.registries.Registries;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.MobCategory;
import net.minecraft.world.level.Level;
import net.neoforged.bus.api.IEventBus;
import net.neoforged.fml.ModContainer;
import net.neoforged.fml.ModList;
import net.neoforged.fml.common.Mod;
import net.neoforged.fml.loading.FMLEnvironment;
import net.neoforged.neoforge.event.entity.EntityAttributeCreationEvent;
import net.neoforged.neoforge.registries.DeferredHolder;
import net.neoforged.neoforge.registries.DeferredRegister;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * MineDeck 伙伴模组的入口（NeoForge 1.21.1）。
 *
 * <p>伙伴的"身体"是 PlayerEngine：寻路（内嵌的 Automatone / Baritone）、任务（采集、跟随、打怪、给东西……）、背包。
 * 这个模组不登录 Player2、不调用 Player2 的网页接口：伙伴没有"主人"（PlayerEngine 那边 owner 为空），PlayerEngine 的
 * 对话层只处理有主人的伙伴，所以它听不到聊天、也不会去调大模型；指令全部来自 MineDeck 的本机连接。
 *
 * <p>客户端和服务端分开：这个类、{@link CompanionEntity}、client 包、{@link JoinCheck}、{@link PlaceholderEntity} 两边都加载，
 * 从不引用 PlayerEngine 的类；挂着 PlayerEngine 的部分（{@link ServerCompanion}、CompanionManager……）只在 PlayerEngine
 * 在的时候由 {@link ServerHooks} 接上。所以玩家的客户端只装本模组就行（PlayerEngine 和 Architectury API 在 mods.toml
 * 里只要求服务端装）。
 */
@Mod(CompanionMod.MODID)
public final class CompanionMod {

    public static final String MODID = "minedeck_companion";
    public static final Logger LOG = LoggerFactory.getLogger(MODID);

    public static final DeferredRegister<EntityType<?>> ENTITIES = DeferredRegister.create(Registries.ENTITY_TYPE, MODID);
    public static final DeferredHolder<EntityType<?>, EntityType<CompanionEntity>> COMPANION = ENTITIES.register("companion",
            () -> EntityType.Builder.<CompanionEntity>of(CompanionMod::create, MobCategory.MISC)
                    .sized(0.6F, 1.8F)
                    .eyeHeight(1.62F)
                    .clientTrackingRange(10)
                    .updateInterval(2)
                    .build(MODID + ":companion"));

    static String version = "0.0.0";
    /** 服务端的实体工厂（PlayerEngine 在时由 ServerHooks 填上）。 */
    static volatile EntityType.EntityFactory<CompanionEntity> serverFactory;
    /** 有没有注册 PlayerEngine 的占位实体类型（没装 PlayerEngine 时）。 */
    static boolean placeholder;

    public CompanionMod(IEventBus modBus, ModContainer container) {
        version = container.getModInfo().getVersion().toString();
        ENTITIES.register(modBus);
        modBus.addListener(CompanionMod::onAttributes);
        modBus.addListener(JoinCheck::onRegisterPayloads);
        modBus.addListener(JoinCheck::onRegisterTasks);
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
