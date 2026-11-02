package minedeck.companion;

import com.player2.playerengine.PlayerEngineController;
import com.player2.playerengine.automaton.api.utils.input.Input;

import net.minecraft.tags.FluidTags;
import net.minecraft.world.entity.LivingEntity;

/**
 * 空闲的伙伴在水里浮上来（照原版生物的 FloatGoal）。
 *
 * <p>PlayerEngine 1.4 防淹水靠 WorldSurvivalChain：它在 getPriority() 里看「在水里、气不够、没在寻路」就按住跳。可是
 * TaskRunner 只在启用时才挨个问各条链的优先级：PlayerEngineController.stop()（MineDeck 的 stop、换任务都会调）把 TaskRunner
 * 停用了，刚建好的控制器在第一次接到任务之前也没启用。这时没人按跳，站在水底的伙伴就淹死了（两半联调时实测：停下来以后
 * 在河边的水里，大约 30 秒）。
 *
 * <p>所以 TaskRunner 没在跑、也没在寻路时，伙伴自己像原版生物一样：水深过了 getFluidJumpThreshold（头快没进水里）、
 * 或者在岩浆里，就按住跳，浮上来以后松开。只动「跳」：不走、不转身，不改 PlayerEngine 的状态，stop 的意思不变
 * （没有任务、没有闲置动作）。有任务时照旧由 PlayerEngine 管（它的寻路会游泳，WorldSurvivalChain 会防淹水）。
 */
final class IdleFloat {
    private IdleFloat() {
    }

    /**
     * 每个服务端 tick 在 PlayerEngine 之后、实体自己 aiStep 之前调用。floating 是上一个 tick 有没有替它按住跳，返回这一个 tick 的。
     * PlayerEngine 只在它的按键变了时才写 jumping，所以我们按下的跳要自己松开（它正按着跳时不松）。
     */
    static boolean tick(LivingEntity e, PlayerEngineController c, boolean floating) {
        boolean idle = c == null || !c.getTaskRunner().isActive() && !c.getBaritone().getPathingBehavior().isPathing();
        boolean deep = e.isInWater() && e.getFluidHeight(FluidTags.WATER) > e.getFluidJumpThreshold() || e.isInLava();
        boolean want = idle && deep && e.isAlive();
        if (want) {
            e.setJumping(true);
        } else if (floating && (c == null || !c.getBaritone().getInputOverrideHandler().isInputForcedDown(Input.JUMP))) {
            e.setJumping(false);
        }
        return want;
    }
}
