package minedeck.companion;

import com.player2.playerengine.automaton.api.entity.LivingEntityInteractionManager;

import minedeck.companion.net.Zones;

import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.network.protocol.game.ServerboundPlayerActionPacket.Action;
import net.minecraft.world.InteractionHand;
import net.minecraft.world.InteractionResult;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.item.BucketItem;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.ClipContext;
import net.minecraft.world.level.Level;
import net.minecraft.world.level.block.DoorBlock;
import net.minecraft.world.level.block.FenceGateBlock;
import net.minecraft.world.level.block.TrapDoorBlock;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.phys.BlockHitResult;
import net.minecraft.world.phys.HitResult;

/**
 * 伙伴的「挖方块、用物品」：PlayerEngine 的 {@link LivingEntityInteractionManager} 加上禁挖区域的检查（{@link ZoneGuard}
 * 的第一层）。PlayerEngine 拆方块、放方块、对方块用物品都经过这里的这几个公开方法，所以在这里拦是最后一道、也是一定
 * 拦得住的一道：区域里的方块不开始挖、不拆（{@code tryBreakBlock} 是唯一调 {@code level.removeBlock} 的地方）、
 * 不对它右键（开关门、活板门、栅栏门除外，而且那时手上的东西不会被放出去）、不往区域里倒水倒岩浆。
 */
final class ZoneInteractionManager extends LivingEntityInteractionManager {

    private final ServerCompanion owner;

    ZoneInteractionManager(ServerCompanion owner) {
        super(owner);
        this.owner = owner;
    }

    private Zones.Zone zoneAt(BlockPos pos) {
        return owner.guard().zoneAt(pos);
    }

    private void refuse(BlockPos pos, Zones.Zone zone, String what) {
        CompanionManager.onZoneRefused(owner, pos.immutable(), zone, what);
    }

    @Override
    public void processBlockBreakingAction(BlockPos pos, Action action, Direction direction, int worldHeight, int i) {
        if (action == Action.START_DESTROY_BLOCK) {
            Zones.Zone zone = zoneAt(pos);
            if (zone != null) {
                refuse(pos, zone, "拆");
                this.world.destroyBlockProgress(this.livingEntity.getId(), pos, -1);
                return;
            }
        }
        super.processBlockBreakingAction(pos, action, direction, worldHeight, i);
    }

    @Override
    public boolean tryBreakBlock(BlockPos pos) {
        Zones.Zone zone = zoneAt(pos);
        if (zone != null) {
            refuse(pos, zone, "拆");
            BlockState state = this.world.getBlockState(pos);
            this.world.sendBlockUpdated(pos, state, state, 3);
            this.world.destroyBlockProgress(this.livingEntity.getId(), pos, -1);
            return false;
        }
        return super.tryBreakBlock(pos);
    }

    @Override
    public InteractionResult interactBlock(LivingEntity player, Level world, ItemStack stack, InteractionHand hand,
                                           BlockHitResult hitResult) {
        BlockPos pos = hitResult.getBlockPos();
        Zones.Zone zone = zoneAt(pos);
        if (zone == null) {
            zone = zoneAt(pos.relative(hitResult.getDirection()));
        }
        if (zone != null) {
            BlockState state = world.getBlockState(pos);
            if (state.getBlock() instanceof DoorBlock || state.getBlock() instanceof TrapDoorBlock
                    || state.getBlock() instanceof FenceGateBlock) {
                // 只让它开关门：手上的东西换成空的，门没反应时也不会把方块放出去
                return super.interactBlock(player, world, ItemStack.EMPTY, hand, hitResult);
            }
            refuse(pos, zone, "动");
            return InteractionResult.FAIL;
        }
        return super.interactBlock(player, world, stack, hand, hitResult);
    }

    @Override
    public InteractionResult interactItem(LivingEntity player, Level world, ItemStack stack, InteractionHand hand) {
        if (stack.getItem() instanceof BucketItem) {
            BlockHitResult hit = raycast(world, player, ClipContext.Fluid.ANY);
            if (hit.getType() == HitResult.Type.BLOCK) {
                BlockPos pos = hit.getBlockPos();
                Zones.Zone zone = zoneAt(pos);
                if (zone == null) {
                    zone = zoneAt(pos.relative(hit.getDirection()));
                }
                if (zone != null) {
                    refuse(pos, zone, "用桶碰");
                    return InteractionResult.FAIL;
                }
            }
        }
        return super.interactItem(player, world, stack, hand);
    }
}
