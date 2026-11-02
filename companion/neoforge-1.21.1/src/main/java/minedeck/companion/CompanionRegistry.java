package minedeck.companion;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.UUID;

import net.minecraft.core.HolderLookup;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.ListTag;
import net.minecraft.nbt.Tag;
import net.minecraft.server.MinecraftServer;
import net.minecraft.world.level.saveddata.SavedData;

/**
 * 世界存档里的伙伴记录（world/data/minedeck_companion.dat，存在主世界的数据里），每条见 {@link RegistryEntry}。
 * 只在服务器主线程上读写；改了要 {@link #setDirty()}，游戏保存世界时一起写盘。
 */
public final class CompanionRegistry extends SavedData {

    static final String NAME = "minedeck_companion";

    final Map<UUID, RegistryEntry> entries = new LinkedHashMap<UUID, RegistryEntry>();

    static CompanionRegistry get(MinecraftServer server) {
        return server.overworld().getDataStorage().computeIfAbsent(
                new SavedData.Factory<CompanionRegistry>(CompanionRegistry::new, CompanionRegistry::load, null), NAME);
    }

    private static CompanionRegistry load(CompoundTag tag, HolderLookup.Provider registries) {
        CompanionRegistry r = new CompanionRegistry();
        ListTag list = tag.getList("Companions", Tag.TAG_COMPOUND);
        for (int i = 0; i < list.size(); i++) {
            RegistryEntry e = RegistryEntry.fromTag(list.getCompound(i));
            if (e != null) {
                r.entries.put(e.uuid, e);
            }
        }
        return r;
    }

    @Override
    public CompoundTag save(CompoundTag tag, HolderLookup.Provider registries) {
        ListTag list = new ListTag();
        for (RegistryEntry e : entries.values()) {
            list.add(e.toTag());
        }
        tag.put("Companions", list);
        tag.putInt("Version", 1);
        return tag;
    }
}
