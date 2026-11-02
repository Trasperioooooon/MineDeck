package minedeck.companion;

import net.minecraft.core.registries.Registries;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.network.syncher.EntityDataAccessor;
import net.minecraft.network.syncher.EntityDataSerializers;
import net.minecraft.network.syncher.SynchedEntityData;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.MobCategory;
import net.minecraft.world.level.Level;
import net.neoforged.bus.api.IEventBus;
import net.neoforged.neoforge.registries.DeferredHolder;
import net.neoforged.neoforge.registries.DeferredRegister;

/**
 * 只在<b>没装 PlayerEngine 的客户端</b>上注册：占住 PlayerEngine 注册的那个实体类型 ID
 * {@code playerengine:custom_fishing_bobber}（它的钓鱼浮标）。
 *
 * <p>进服时服务器会把所有实体类型的 ID 同步给客户端，客户端缺了哪一个，NeoForge 就拒绝进服。PlayerEngine 只注册了这一个
 * 要同步的东西（另外几个物品标签不要求客户端认识），所以客户端有了这个占位，就不用装 PlayerEngine 和 Architectury API。
 * 伙伴从不钓鱼（MineDeck 的动作清单里没有），这个实体正常不会出现；万一出现了，它的同步数据和浮标一样
 * （一个整数、一个布尔值），客户端不会因此出错，只是什么都不画。
 */
public final class PlaceholderEntity extends Entity {

    static final String NAMESPACE = "playerengine";
    static final String PATH = "custom_fishing_bobber";

    private static final EntityDataAccessor<Integer> HOOK = SynchedEntityData.defineId(PlaceholderEntity.class,
            EntityDataSerializers.INT);
    private static final EntityDataAccessor<Boolean> CAUGHT = SynchedEntityData.defineId(PlaceholderEntity.class,
            EntityDataSerializers.BOOLEAN);

    static DeferredHolder<EntityType<?>, EntityType<PlaceholderEntity>> TYPE;

    public PlaceholderEntity(EntityType<? extends PlaceholderEntity> type, Level level) {
        super(type, level);
        noPhysics = true;
    }

    /** CompanionMod 在 PlayerEngine 不在时调用。 */
    static void register(IEventBus modBus) {
        DeferredRegister<EntityType<?>> reg = DeferredRegister.create(Registries.ENTITY_TYPE, NAMESPACE);
        TYPE = reg.register(PATH, () -> EntityType.Builder.<PlaceholderEntity>of(PlaceholderEntity::new, MobCategory.MISC)
                .sized(0.25F, 0.25F)
                .clientTrackingRange(4)
                .updateInterval(5)
                .noSave()
                .noSummon()
                .build(NAMESPACE + ":" + PATH));
        reg.register(modBus);
    }

    @Override
    protected void defineSynchedData(SynchedEntityData.Builder builder) {
        builder.define(HOOK, 0);
        builder.define(CAUGHT, false);
    }

    @Override
    protected void readAdditionalSaveData(CompoundTag tag) {
    }

    @Override
    protected void addAdditionalSaveData(CompoundTag tag) {
    }
}
