package minedeck.companion;

import java.util.Set;
import java.util.function.Predicate;

import com.player2.playerengine.tasks.base.Task;
import com.player2.playerengine.tasks.entity.KillEntitiesTask;

import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.entity.OwnableEntity;
import net.minecraft.world.entity.monster.Enemy;
import net.minecraft.world.entity.player.Player;
import net.minecraft.world.phys.AABB;
import net.minecraft.world.phys.Vec3;

/**
 * attack：只打某一种生物，只打以开始时伙伴的位置为中心、radius 以内的，打够 count 只或者打满 duration 秒就结束。
 * 真正去打的是 PlayerEngine 的 {@link KillEntitiesTask}（找最近的、走过去、打），这里只决定打谁、什么时候停。
 *
 * <p>永远不打：玩家、MineDeck 的伙伴、有主人的动物、起了名字的、拴着的生物，以及配置里 attack_deny 列出的种类。
 * 这一条在伙伴的 doHurtTarget 里也把着（{@link #mayHit}），所以伙伴不管为什么出手都打不伤它们。唯一的例外是自卫：
 * 正在打伙伴的敌对生物（比如被同伴叫来的、起了名字的僵尸）可以还手，玩家和伙伴永远不行。
 * 打死了几只由 {@link CompanionManager#onLivingDeath} 按伤害来源数。
 */
final class AttackTask extends Task {

    /** 只给 count（没给 duration）时，范围里这么久没有目标就算结束（结果看打够没有）。 */
    static final long NO_TARGET_MS = 15_000L;

    final EntityType<?> type;
    final String typeId;
    final Vec3 anchor;
    final double radius;
    final int count;
    final long endAt;
    private final Set<String> deny;
    private final Predicate<Entity> target = this::isTarget;
    private final KillEntitiesTask kill;
    int killed;
    private long lastSeen = System.currentTimeMillis();

    AttackTask(EntityType<?> type, String typeId, Vec3 anchor, double radius, int count, long endAt, Set<String> deny) {
        this.type = type;
        this.typeId = typeId;
        this.anchor = anchor;
        this.radius = radius;
        this.count = count;
        this.endAt = endAt;
        this.deny = deny;
        this.kill = new KillEntitiesTask(target);
    }

    /** 种类本身能不能打（开始前检查）；不能打时返回原因。 */
    static String forbiddenType(EntityType<?> type, String typeId, Set<String> deny) {
        if (type == EntityType.PLAYER) {
            return "不打玩家";
        }
        if (typeId.startsWith(CompanionMod.MODID + ":")) {
            return "不打伙伴";
        }
        if (deny.contains(typeId)) {
            return "配置的 attack_deny 里不许打 " + typeId;
        }
        return null;
    }

    /** 个别生物能不能打：同种类里起了名字的、有主人的、拴着的都跳过。 */
    static boolean protectedEntity(Entity e) {
        if (e instanceof Player || e instanceof CompanionEntity) {
            return true;
        }
        if (e.hasCustomName()) {
            return true;
        }
        if (e instanceof OwnableEntity && ((OwnableEntity) e).getOwnerUUID() != null) {
            return true;
        }
        return e instanceof Mob && ((Mob) e).isLeashed();
    }

    boolean isTarget(Entity e) {
        if (!(e instanceof LivingEntity) || !e.isAlive() || e.position().distanceToSqr(anchor) > radius * radius) {
            return false;
        }
        LivingEntity self = controller == null ? null : controller.getEntity();
        if (self != null && e instanceof Enemy && attacking(self, e) && !(e instanceof Player) && !(e instanceof CompanionEntity)) {
            return true; // 自卫：正在打伙伴的敌对生物，不管是什么种类、起没起名字
        }
        if (e.getType() != type || protectedEntity(e)) {
            return false;
        }
        return !deny.contains(BuiltInRegistries.ENTITY_TYPE.getKey(e.getType()).toString());
    }

    /** target 正在打 self：把 self 当成目标，或者 10 秒内打过 self。 */
    static boolean attacking(LivingEntity self, Entity target) {
        if (target instanceof Mob && ((Mob) target).getTarget() == self) {
            return true;
        }
        return self.getLastHurtByMob() == target && self.tickCount - self.getLastHurtByMobTimestamp() < 200;
    }

    /** 伙伴能不能打伤它（伙伴的 doHurtTarget 用）：受保护的不行，除非是正在打伙伴的敌对生物；玩家和伙伴永远不行。 */
    static boolean mayHit(LivingEntity self, Entity target) {
        if (target instanceof Player || target instanceof CompanionEntity) {
            return false;
        }
        if (!protectedEntity(target) && !CompanionManager.attackDenied(target)) {
            return true;
        }
        return target instanceof Enemy && attacking(self, target);
    }

    boolean hasTarget() {
        if (controller == null) {
            return false;
        }
        AABB box = new AABB(anchor.x - radius, anchor.y - radius, anchor.z - radius,
                anchor.x + radius, anchor.y + radius, anchor.z + radius);
        return !controller.getWorld().getEntitiesOfClass(LivingEntity.class, box, this::isTarget).isEmpty();
    }

    /** 这只是不是它这次该数的（打死时调用）。 */
    boolean counts(LivingEntity victim) {
        return victim.getType() == type && !protectedEntity(victim)
                && victim.position().distanceToSqr(anchor) <= (radius + 4) * (radius + 4);
    }

    @Override
    protected void onStart() {
        lastSeen = System.currentTimeMillis();
    }

    @Override
    protected Task onTick() {
        if (isFinished()) {
            return null;
        }
        if (hasTarget()) {
            lastSeen = System.currentTimeMillis();
            setDebugState("在打 " + typeId);
            return kill;
        }
        setDebugState("等 " + typeId + " 进入范围");
        return null;
    }

    @Override
    protected void onStop(Task interruptTask) {
    }

    @Override
    public boolean isFinished() {
        long now = System.currentTimeMillis();
        if (count > 0 && killed >= count) {
            return true;
        }
        if (endAt > 0) {
            return now >= endAt;
        }
        return now - lastSeen > NO_TARGET_MS;
    }

    @Override
    protected boolean isEqual(Task other) {
        return other == this;
    }

    @Override
    protected String toDebugString() {
        return "MineDeck attack " + typeId + " (" + killed + (count > 0 ? "/" + count : "") + ")";
    }
}
