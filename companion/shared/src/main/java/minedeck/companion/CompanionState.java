package minedeck.companion;

import java.util.UUID;

import com.google.gson.JsonObject;

import net.minecraft.world.phys.Vec3;

/** 管理器这边记着的一个伙伴（编号、主人、当前任务……）。只在服务器主线程上读写。 */
final class CompanionState {
    final int id;
    final UUID uuid;
    String name;
    String owner;
    String ownerUuid;
    String tag;
    /** 现在的实体；还没加载（或者换维度、重启的间隙里）为 null。 */
    ServerCompanion entity;
    /** 最后已知的位置（实体不在时 status 用）。 */
    String dim = "minecraft:overworld";
    double x;
    double y;
    double z;
    float health;
    float maxHealth;

    TaskRun task;
    /** 最近一次 task_finished 的内容（不含 event、companion），加上 at。 */
    JsonObject lastResult;

    // 卡住检测：参考位置和时间，动了超过 1.5 格就重置
    Vec3 refPos;
    long refAt;
    int refCount = -1;
    boolean stuckReported;
    boolean nodigReported;

    CompanionState(int id, UUID uuid) {
        this.id = id;
        this.uuid = uuid;
    }

    void remember(ServerCompanion e) {
        entity = e;
        name = e.getName().getString();
        dim = e.level().dimension().location().toString();
        x = e.getX();
        y = e.getY();
        z = e.getZ();
        health = e.getHealth();
        maxHealth = e.getMaxHealth();
    }

    /** 实体还在世界里、能用。 */
    ServerCompanion live() {
        ServerCompanion e = entity;
        return e != null && !e.isRemoved() && e.isAlive() ? e : null;
    }
}
