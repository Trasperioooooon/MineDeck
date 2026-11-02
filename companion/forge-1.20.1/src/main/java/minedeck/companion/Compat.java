package minedeck.companion;

import java.nio.file.Path;

import net.minecraft.resources.ResourceLocation;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.level.ForcedChunksSavedData;
import net.minecraftforge.fml.ModList;
import net.minecraftforge.fml.loading.FMLPaths;

/** 共用代码（companion/shared/）里和加载器、MC 版本有关的几处，Forge 1.20.1 的写法。 */
final class Compat {

    static final String LOADER = "forge";

    private Compat() {
    }

    @SuppressWarnings("removal")
    static ResourceLocation rl(String namespace, String path) {
        return new ResourceLocation(namespace, path);
    }

    static ResourceLocation tryParse(String id) {
        return ResourceLocation.tryParse(id);
    }

    static String modVersion(String modId) {
        return ModList.get().getModContainerById(modId).map(c -> c.getModInfo().getVersion().toString()).orElse("?");
    }

    static Path gameDir() {
        return FMLPaths.GAMEDIR.get();
    }

    /**
     * 让这个维度的强制加载区块记录（data/chunks.dat）在下次存档时写出去。原版 setChunkForced 每次都把「要保存」
     * 设成「这一次有没有改动」，所以重复加、重复放一次（PlayerEngine 的区块记账经常这样），之前的改动就不保存了，
     * 重启后伙伴脚下的区块不再强制加载。
     */
    static void markForcedChunksDirty(ServerLevel level) {
        ForcedChunksSavedData data = level.getDataStorage().get(ForcedChunksSavedData::load, ForcedChunksSavedData.FILE_ID);
        if (data != null) {
            data.setDirty();
        }
    }

    static EntityType<CompanionEntity> companionType() {
        return CompanionMod.COMPANION.get();
    }
}
