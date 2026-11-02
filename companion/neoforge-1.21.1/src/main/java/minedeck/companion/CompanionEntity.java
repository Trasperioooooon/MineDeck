package minedeck.companion;

import net.minecraft.core.NonNullList;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.world.entity.HumanoidArm;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.ai.attributes.AttributeSupplier;
import net.minecraft.world.entity.ai.attributes.Attributes;
import net.minecraft.world.entity.player.Player;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.Level;

/**
 * 伙伴实体在两边都有的部分：客户端只认得它的外形（玩家模型、名字、手上和身上的装备），不引用 PlayerEngine 的任何类，
 * 所以玩家的客户端不用装 PlayerEngine。
 *
 * <p>服务端用的是子类 {@link ServerCompanion}（挂着 PlayerEngine），由实体类型的工厂在服务端创建
 * （见 {@link CompanionMod#create}）。客户端收到的装备由原版的装备包同步过来，存在这里的两个列表里。
 */
public class CompanionEntity extends LivingEntity {

    /**
     * PlayerEngine 前进时把 zza 设成 0.3（玩家按住 W 是 0.98），速度属性按比例放大到 0.33，
     * 实际走路速度和玩家差不多（推断：没有逐 tick 对比过）。
     */
    static final double WALK_SPEED = 0.33;

    private final NonNullList<ItemStack> hands = NonNullList.withSize(2, ItemStack.EMPTY);
    private final NonNullList<ItemStack> armor = NonNullList.withSize(4, ItemStack.EMPTY);

    public CompanionEntity(EntityType<? extends CompanionEntity> type, Level level) {
        super(type, level);
    }

    public static AttributeSupplier.Builder createAttributes() {
        // 用玩家的整套属性（攻击、挖掘速度、交互距离……），只改移动速度
        return Player.createAttributes().add(Attributes.MOVEMENT_SPEED, WALK_SPEED);
    }

    @Override
    public Iterable<ItemStack> getArmorSlots() {
        return armor;
    }

    @Override
    public ItemStack getItemBySlot(EquipmentSlot slot) {
        switch (slot.getType()) {
            case HAND:
                return hands.get(slot.getIndex());
            case HUMANOID_ARMOR:
                return armor.get(slot.getIndex());
            default:
                return ItemStack.EMPTY;
        }
    }

    @Override
    public void setItemSlot(EquipmentSlot slot, ItemStack stack) {
        switch (slot.getType()) {
            case HAND:
                hands.set(slot.getIndex(), stack);
                break;
            case HUMANOID_ARMOR:
                armor.set(slot.getIndex(), stack);
                break;
            default:
                break;
        }
    }

    @Override
    public HumanoidArm getMainArm() {
        return HumanoidArm.RIGHT;
    }

    @Override
    public void aiStep() {
        super.aiStep();
        updateSwingTime();
    }
}
