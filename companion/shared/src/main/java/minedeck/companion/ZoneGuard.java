package minedeck.companion;

import java.util.ArrayDeque;
import java.util.List;
import java.util.function.Predicate;

import com.player2.playerengine.BotBehaviour;
import com.player2.playerengine.PlayerEngineController;
import com.player2.playerengine.automaton.AdditionalBaritoneSettings;
import com.player2.playerengine.automaton.Baritone;
import com.player2.playerengine.automaton.api.Settings;
import com.player2.playerengine.automaton.api.pathing.movement.IMovement;
import com.player2.playerengine.automaton.behavior.PathingBehavior;
import com.player2.playerengine.automaton.pathing.movement.Movement;
import com.player2.playerengine.automaton.pathing.path.PathExecutor;
import com.player2.playerengine.automaton.utils.BlockStateInterface;

import minedeck.companion.net.Zones;

import net.minecraft.core.BlockPos;

/**
 * 一个伙伴的禁挖区域防护，三层：
 *
 * <ol>
 *   <li><b>动作层（兜底，一定拦得住）</b>：{@link ZoneInteractionManager} 拦下拆、放区域里的方块和对它们用物品。
 *       PlayerEngine 拆方块走的是 {@code level.removeBlock}，不发拆方块事件，领地保护模组拦不住，只能在这里拦。</li>
 *   <li><b>任务层</b>：给 PlayerEngine 的「别拆」「别放」判断加上区域（{@code BotBehaviour.avoidBlockBreaking /
 *       avoidBlockPlacing}），采集选目标时就跳过区域里的方块。PlayerEngine 的行为状态是个栈，弹栈会把这两个列表恢复成
 *       压栈时的样子，所以每秒、每次派任务前都检查一遍，丢了就补上。</li>
 *   <li><b>寻路层</b>：PlayerEngine 1.4 的寻路只看 allowBreak / allowPlace 和「玩家放的方块」，没有按位置禁止的接口。
 *       所以每 5 tick 看一遍当前路径（和规划好的下一段）要拆、要放的方块，碰到区域里的，就把这个伙伴切到「只走路」：
 *       allowBreak、allowPlace 都关掉，取消当前路径让它重新规划，绕过去。离开所有区域 8 格以外、过了 3 秒再恢复。
 *       动作层拦下了拆放时也切到「只走路」。</li>
 * </ol>
 */
final class ZoneGuard {

    static final long WALK_ONLY_MIN_MS = 3000L;
    static final double WALK_ONLY_EXIT_DISTANCE_SQ = 8.0 * 8.0;
    static final long REFUSAL_WINDOW_MS = 30_000L;
    static final int REFUSALS_FOR_STUCK = 5;
    /**
     * 只在测试时用：JVM 参数 -Dminedeck.companion.debug.onlyActionGuard=true 关掉任务层和寻路层，只留动作层，
     * 用来实测「PlayerEngine 真要拆区域里的方块时也拆不掉」。正常运行时不要开。
     */
    static final boolean ONLY_ACTION_GUARD = Boolean.getBoolean("minedeck.companion.debug.onlyActionGuard");

    private final ServerCompanion entity;
    final Predicate<BlockPos> protects = this::protects;
    private boolean walkOnly;
    private long walkOnlySince;
    private Zones.Zone lastZone;
    private final ArrayDeque<Long> refusals = new ArrayDeque<Long>();
    private long lastRefusalLog;

    ZoneGuard(ServerCompanion entity) {
        this.entity = entity;
    }

    String dim() {
        return entity.level().dimension().location().toString();
    }

    /** 这个方块在伙伴所在维度的某个禁挖区域里（可能在寻路线程上调用：只读不可变的区域快照）。 */
    boolean protects(BlockPos pos) {
        return !ONLY_ACTION_GUARD && zoneAt(pos) != null;
    }

    Zones.Zone zoneAt(BlockPos pos) {
        Zones zones = CompanionManager.zones();
        return zones.isEmpty() ? null : zones.find(dim(), pos.getX(), pos.getY(), pos.getZ());
    }

    boolean walkOnly() {
        return walkOnly;
    }

    Zones.Zone lastZone() {
        return lastZone;
    }

    /** 任务层：确认两个判断还在 PlayerEngine 的列表里，丢了（弹栈恢复了旧状态）就补上。主线程上调用。 */
    void ensureAvoiders(PlayerEngineController c) {
        AdditionalBaritoneSettings extra = c.getExtraBaritoneSettings();
        boolean hasBreak;
        boolean hasPlace;
        synchronized (extra.getBreakMutex()) {
            hasBreak = extra.getBreakAvoiders().contains(protects);
        }
        synchronized (extra.getPlaceMutex()) {
            hasPlace = extra.getPlaceAvoiders().contains(protects);
        }
        BotBehaviour b = c.getBehaviour();
        if (!hasBreak) {
            b.avoidBlockBreaking(protects);
        }
        if (!hasPlace) {
            b.avoidBlockPlacing(protects);
        }
    }

    /** 派新任务前：补上判断，回到正常寻路（新任务从头规划，碰到区域会再切过去）。 */
    void beforeTask(PlayerEngineController c) {
        ensureAvoiders(c);
        if (walkOnly) {
            restore(c, "换了新任务");
        }
        refusals.clear();
    }

    /** 每 5 tick（主线程）：看路径；「只走路」时看能不能恢复。 */
    void tick(PlayerEngineController c, long now) {
        Zones zones = CompanionManager.zones();
        String dim = dim();
        if (walkOnly) {
            if (now - walkOnlySince >= WALK_ONLY_MIN_MS
                    && zones.distanceSq(dim, entity.getX(), entity.getY(), entity.getZ()) > WALK_ONLY_EXIT_DISTANCE_SQ) {
                restore(c, "离禁挖区域已经够远");
            }
            return;
        }
        if (ONLY_ACTION_GUARD || zones.isEmpty()
                || zones.distanceSq(dim, entity.getX(), entity.getY(), entity.getZ()) > 256.0 * 256.0) {
            return;
        }
        PathingBehavior pb = ((Baritone) c.getBaritone()).getPathingBehavior();
        BlockStateInterface bsi = new BlockStateInterface(c.getBaritone().getEntityContext());
        Hit hit = scan(pb.getCurrent(), bsi, zones, dim);
        if (hit == null) {
            hit = scan(pb.getNext(), bsi, zones, dim);
        }
        if (hit != null) {
            enterWalkOnly(c, hit.zone, "规划的路要" + hit.what + "禁挖区域「" + hit.zone.name + "」里的方块 "
                    + hit.pos.toShortString());
        }
    }

    private static final class Hit {
        final Zones.Zone zone;
        final BlockPos pos;
        final String what;

        Hit(Zones.Zone zone, BlockPos pos, String what) {
            this.zone = zone;
            this.pos = pos;
            this.what = what;
        }
    }

    private static Hit scan(PathExecutor exec, BlockStateInterface bsi, Zones zones, String dim) {
        if (exec == null) {
            return null;
        }
        List<IMovement> moves = exec.getPath().movements();
        for (int i = Math.max(0, exec.getPosition()); i < moves.size(); i++) {
            IMovement im = moves.get(i);
            if (!(im instanceof Movement)) {
                continue;
            }
            Movement m = (Movement) im;
            for (BlockPos p : m.toBreak(bsi)) {
                Zones.Zone z = zones.find(dim, p.getX(), p.getY(), p.getZ());
                if (z != null) {
                    return new Hit(z, p, "拆");
                }
            }
            for (BlockPos p : m.toPlace(bsi)) {
                Zones.Zone z = zones.find(dim, p.getX(), p.getY(), p.getZ());
                if (z != null) {
                    return new Hit(z, p, "放");
                }
            }
        }
        return null;
    }

    /** 动作层拦下了一次（主线程）。返回最近 30 秒里拦下了几次。 */
    int onRefused(PlayerEngineController c, BlockPos pos, Zones.Zone zone, String what, long now) {
        refusals.addLast(now);
        while (!refusals.isEmpty() && now - refusals.peekFirst() > REFUSAL_WINDOW_MS) {
            refusals.removeFirst();
        }
        lastZone = zone;
        if (now - lastRefusalLog > 10_000L) {
            lastRefusalLog = now;
            CompanionManager.LOG.info("伙伴「{}」想{}禁挖区域「{}」里的方块 {}，已拦下", entity.getName().getString(), what,
                    zone.name, pos.toShortString());
        }
        if (c != null && !walkOnly && !ONLY_ACTION_GUARD) {
            enterWalkOnly(c, zone, "想" + what + "禁挖区域「" + zone.name + "」里的方块 " + pos.toShortString() + "，被拦下");
        }
        return refusals.size();
    }

    private void enterWalkOnly(PlayerEngineController c, Zones.Zone zone, String why) {
        Settings s = c.getBaritoneSettings();
        s.allowBreak.set(false);
        s.allowPlace.set(false);
        walkOnly = true;
        walkOnlySince = System.currentTimeMillis();
        lastZone = zone;
        try {
            ((Baritone) c.getBaritone()).getPathingBehavior().forceCancel();
        } catch (RuntimeException e) {
            CompanionManager.LOG.warn("取消伙伴的路径时出错：{}", e.toString());
        }
        CompanionManager.LOG.info("伙伴「{}」{}，改走不拆不放的路", entity.getName().getString(), why);
    }

    void restore(PlayerEngineController c, String why) {
        if (!walkOnly) {
            return;
        }
        Settings s = c.getBaritoneSettings();
        s.allowBreak.set(true);
        s.allowPlace.set(true);
        walkOnly = false;
        CompanionManager.LOG.info("伙伴「{}」{}，恢复正常寻路", entity.getName().getString(), why);
    }
}
