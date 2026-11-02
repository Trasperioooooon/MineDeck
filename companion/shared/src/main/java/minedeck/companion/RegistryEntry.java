package minedeck.companion;

import java.util.Arrays;
import java.util.UUID;

import net.minecraft.nbt.CompoundTag;

/**
 * 存进世界存档（data/minedeck_companion.dat，见 {@link CompanionRegistry}）的一条伙伴记录：编号、名字、最后的位置，
 * 以及 PlayerEngine 替它强制加载过、可能还没放开的区块。
 *
 * <p>用处：重启后知道应该有哪些伙伴（编号不会和还没加载的伙伴撞上）；找不到的伙伴（比如不装本模组开过一次服，
 * 实体被游戏丢掉了）要清掉它留下的强制加载区块。
 */
final class RegistryEntry {
    final UUID uuid;
    int id;
    String name;
    String dim;
    double x;
    double y;
    double z;
    /** 替这个伙伴强制加载过、还没放开的区块（ChunkPos.toLong() 的值，在 dim 里）。 */
    long[] chunks = new long[0];

    RegistryEntry(UUID uuid) {
        this.uuid = uuid;
    }

    CompoundTag toTag() {
        CompoundTag t = new CompoundTag();
        t.putUUID("Uuid", uuid);
        t.putInt("Id", id);
        t.putString("Name", name == null ? "" : name);
        t.putString("Dim", dim == null ? "" : dim);
        t.putDouble("X", x);
        t.putDouble("Y", y);
        t.putDouble("Z", z);
        t.putLongArray("Chunks", chunks);
        return t;
    }

    static RegistryEntry fromTag(CompoundTag t) {
        if (!t.hasUUID("Uuid")) {
            return null;
        }
        RegistryEntry e = new RegistryEntry(t.getUUID("Uuid"));
        e.id = t.getInt("Id");
        e.name = t.getString("Name");
        e.dim = t.getString("Dim");
        e.x = t.getDouble("X");
        e.y = t.getDouble("Y");
        e.z = t.getDouble("Z");
        e.chunks = t.getLongArray("Chunks");
        return e;
    }

    /** 更新位置和区块记录；有变化时返回 true（要 setDirty）。 */
    boolean update(String name, String dim, double x, double y, double z, long[] chunks) {
        boolean changed = !dim.equals(this.dim) || !name.equals(this.name) || !Arrays.equals(chunks, this.chunks)
                || Math.abs(x - this.x) > 1 || Math.abs(y - this.y) > 1 || Math.abs(z - this.z) > 1;
        this.name = name;
        this.dim = dim;
        this.x = x;
        this.y = y;
        this.z = z;
        this.chunks = chunks;
        return changed;
    }
}
