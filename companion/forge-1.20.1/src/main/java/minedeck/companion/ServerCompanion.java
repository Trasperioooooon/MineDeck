package minedeck.companion;

import java.util.List;
import java.util.UUID;

import com.player2.playerengine.PlayerEngineController;
import com.player2.playerengine.automaton.api.IBaritone;
import com.player2.playerengine.automaton.api.entity.IAutomatone;
import com.player2.playerengine.automaton.api.entity.IHungerManagerProvider;
import com.player2.playerengine.automaton.api.entity.IInteractionManagerProvider;
import com.player2.playerengine.automaton.api.entity.IInventoryProvider;
import com.player2.playerengine.automaton.api.entity.LivingEntityHungerManager;
import com.player2.playerengine.automaton.api.entity.LivingEntityInteractionManager;
import com.player2.playerengine.automaton.api.entity.LivingEntityInventory;
import com.player2.playerengine.player2api.Character;
import com.player2.playerengine.player2api.manager.ConversationManager;
import com.player2.playerengine.util.ChunkController;

import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.ListTag;
import net.minecraft.nbt.Tag;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.sounds.SoundEvents;
import net.minecraft.util.Mth;
import net.minecraft.world.damagesource.DamageSource;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.ai.attributes.Attributes;
import net.minecraft.world.entity.item.ItemEntity;
import net.minecraft.world.entity.player.Player;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.item.enchantment.EnchantmentHelper;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.Level;

/**
 * 伙伴实体的服务端部分：挂着 PlayerEngine 的「身体」。只在服务端创建（实体类型的工厂按 level.isClientSide 区分），
 * 客户端从不加载这个类，所以客户端不用装 PlayerEngine。
 *
 * <p>PlayerEngine 把「玩家的能力」做成接口：实现 {@link IAutomatone}（拿到内嵌的 Automatone/Baritone）、
 * {@link IInventoryProvider}（玩家式背包）、{@link IInteractionManagerProvider}（挖方块、用物品，换成了带禁挖区域检查的
 * {@link ZoneInteractionManager}）、{@link IHungerManagerProvider}（饥饿，PlayerEngine 1.4 默认关闭）。每个 tick 推进
 * 挖掘进度、背包物品、PlayerEngine 的任务和寻路；手上和身上的装备直接读写背包；捡起附近的掉落物。PlayerEngine 通过
 * xxa / zza / jumping 驱动移动，LivingEntity 自己的 aiStep → travel 让它真正走起来。
 */
public class ServerCompanion extends CompanionEntity
        implements IAutomatone, IInventoryProvider, IInteractionManagerProvider, IHungerManagerProvider {

    /** 交给 PlayerEngine 的"游戏 ID"：PlayerEngine 只拿它去 Player2 换令牌，我们从不让它走到那一步。 */
    static final String CLIENT_ID = "minedeck-companion";
    private static final int CONTROLLER_ATTEMPTS = 3;

    private final LivingEntityInventory inventory = new LivingEntityInventory(this);
    private final ZoneInteractionManager interactionManager = new ZoneInteractionManager(this);
    private final LivingEntityHungerManager hungerManager = new LivingEntityHungerManager();
    private final ZoneGuard guard = new ZoneGuard(this);
    private PlayerEngineController controller;
    private int controllerAttempts;
    /** 第一次服务端 tick 开始了。PlayerEngine 控制器要在这之后才能建：实体还没 tick 过时建的控制器不会动（实测）。 */
    private boolean ticked;
    private boolean adopted;
    private long lastTickError;
    /** 上一个 tick 替它按住了跳（空闲时在水里浮上来，见 {@link IdleFloat}）。 */
    private boolean floating;

    // MineDeck 的记录（存进实体的 NBT）
    private int companionId;
    private String owner;
    private String ownerUuid;
    private String tag;

    /** 刚由 spawn 指令刷出（不发 adopted 事件）。 */
    boolean freshSpawn;
    /** 是 MineDeck 的 despawn 移除的（removed 事件的 reason 是 despawn）。 */
    boolean despawnRequested;

    public ServerCompanion(EntityType<? extends CompanionEntity> type, Level level) {
        super(type, level);
    }

    /** PlayerEngine 控制器；第一次 tick（或者 spawn 之后马上）创建，还没创建时为 null。 */
    public PlayerEngineController controller() {
        return controller;
    }

    ZoneGuard guard() {
        return guard;
    }

    int companionId() {
        return companionId;
    }

    String owner() {
        return owner;
    }

    String ownerUuid() {
        return ownerUuid;
    }

    String tag() {
        return tag;
    }

    void setMeta(int id, String owner, String ownerUuid, String tag) {
        this.companionId = id;
        this.owner = owner;
        this.ownerUuid = ownerUuid;
        this.tag = tag;
    }

    @Override
    public LivingEntityInventory getLivingInventory() {
        return inventory;
    }

    @Override
    public LivingEntityInteractionManager getInteractionManager() {
        return interactionManager;
    }

    @Override
    public LivingEntityHungerManager getHungerManager() {
        return hungerManager;
    }

    @Override
    public void tick() {
        if (!level().isClientSide && isAlive()) {
            serverPreTick();
        }
        super.tick();
    }

    private void serverPreTick() {
        ticked = true;
        // 攻击冷却：原版只在 Player.tick 里加这个计数，PlayerEngine 打怪前要看它（够 5 才出手），伙伴不是玩家，自己加
        attackStrengthTicker++;
        if (!adopted) {
            adopted = true;
            CompanionManager.adopt(this);
        }
        ensureController();
        interactionManager.update();
        inventory.updateItems();
        if (controller != null) {
            try {
                controller.serverTick();
            } catch (Throwable t) {
                tickError("PlayerEngine 的 tick 出错", t);
            }
        }
        try {
            floating = IdleFloat.tick(this, controller, floating);
        } catch (Throwable t) {
            tickError("空闲时在水里浮起来出错", t);
        }
        setYHeadRot(getYRot());
    }

    /** tick 里出的错只记下来（5 秒内只记一次），不让它变成整个服务器崩溃。 */
    private void tickError(String what, Throwable t) {
        long now = System.currentTimeMillis();
        if (now - lastTickError > 5000) {
            lastTickError = now;
            CompanionManager.LOG.error(what + "（5 秒内只记一次）", t);
        }
    }

    /**
     * 还没有 PlayerEngine 控制器就创建一个；返回现在有没有。实体第一次 tick 之前不建（刚刷出的伙伴要等一个 tick，
     * 这期间的指令回 not_ready）；失败 3 次就不再试（伙伴不会动）。
     */
    boolean ensureController() {
        if (controller != null) {
            return true;
        }
        if (level().isClientSide || !ticked || controllerAttempts >= CONTROLLER_ATTEMPTS) {
            return false;
        }
        controllerAttempts++;
        try {
            controller = createController();
            CompanionManager.LOG.info("伙伴「{}」的 PlayerEngine 控制器已创建（不登录 Player2，对话层已关闭）", getName().getString());
            return true;
        } catch (Throwable t) {
            CompanionManager.LOG.error("创建 PlayerEngine 控制器失败（第 " + controllerAttempts + " 次）", t);
            return false;
        }
    }

    private PlayerEngineController createController() {
        String name = getName().getString();
        Character character = new Character(CLIENT_ID, name, name, "", "MineDeck companion", "", new String[0]);
        // owner 传 null：PlayerEngine 的对话层（ConversationManager.process）只处理有主人的伙伴，
        // 心跳也只在主人有 Player2 令牌时才发。再把这个伙伴的对话队列关掉，双保险。
        // MineDeck 记的主人（owner()）只是我们自己的标记，不交给 PlayerEngine。
        PlayerEngineController c = new PlayerEngineController(IBaritone.KEY.get(this), character, CLIENT_ID, null);
        ConversationManager.getOrCreateEventQueueData(c).setEnabled(false);
        // /playerengine relog 会对这张表里每个伙伴发起 Player2 登录（连 api.player2.game）。
        // 我们的伙伴从表里拿掉，这条命令就碰不到它（表只给 PlayerEngine 自己的管理命令用）
        PlayerEngineController.staticAPIServices.remove(getUUID());
        // 关掉 PlayerEngine 的自动防御（MobDefenseChain：见到附近的敌对生物就去打、打不过就跑，起了名字的也打）。
        // 它的优先级比 MineDeck 交的任务高，会把任务晾在一边；而怪物一般不主动打伙伴（它不是玩家）。
        // 伙伴只在 MineDeck 让它 attack 时出手（这个方法名是 PlayerEngine 起的，true 才是「不防御」）
        c.setShouldDefendFromHostiles(true);
        guard.ensureAvoiders(c);
        return c;
    }

    @Override
    public void aiStep() {
        if (!level().isClientSide) {
            // 玩家每 tick 按速度属性设一次速度；PlayerEngine 只在寻路时设（它的 mixin），平时也要有
            setSpeed((float) getAttributeValue(Attributes.MOVEMENT_SPEED));
        }
        super.aiStep();
        if (!level().isClientSide && isAlive()) {
            pickUpItems();
        }
    }

    /**
     * 伙伴不捡的东西：玩家扔的（免得跟着玩家时把他扔给朋友的东西抢走）、伙伴扔的（give 交给玩家的、伙伴死了掉的）。
     * 挖方块、打怪掉的东西没有扔的人，照常捡。
     */
    private static boolean mayPickUp(ItemEntity item) {
        if (item.getTags().contains(DROP_TAG)) {
            return false;
        }
        Entity owner = item.getOwner();
        return !(owner instanceof Player) && !(owner instanceof CompanionEntity);
    }

    /** 伙伴扔出的东西（give、死了、despawn 带 drop）都打这个标记，伙伴们不再捡回来。 */
    static final String DROP_TAG = "minedeck_companion_drop";

    /**
     * PlayerEngine 的 give 用这个方法把东西扔在伙伴脚下。这里补上：记下是谁扔的；正在 give 时只让接东西的玩家捡得起来
     * （别的玩家、伙伴都不行），并且朝他扔过去，免得落在伙伴脚下、玩家够不着。
     */
    @Override
    public ItemEntity spawnAtLocation(ItemStack stack, float yOffset) {
        ItemEntity item = super.spawnAtLocation(stack, yOffset);
        if (item == null || level().isClientSide) {
            return item;
        }
        item.setThrower(getUUID());
        item.addTag(DROP_TAG);
        CompanionManager.noteThrown(this, item.getItem());
        ServerPlayer to = CompanionManager.giveTarget(this);
        if (to != null && to.level() == level()) {
            item.setTarget(to.getUUID());
            double dx = to.getX() - item.getX();
            double dz = to.getZ() - item.getZ();
            double dist = Math.sqrt(dx * dx + dz * dz);
            if (dist > 0.5) {
                // 掉落物带着 0.25 的向上速度扔出去，连飞带滑大约走 13.4 × 水平速度 格（空气阻力 0.98、草地摩擦 0.6），
                // 按这个算，落在玩家脚边稍近一点的地方（玩家伸手捡得到的范围是 1 格多）
                double speed = Math.min(0.5, dist / 14.0);
                item.setDeltaMovement(dx / dist * speed, 0.25, dz / dist * speed);
            }
        }
        return item;
    }

    /** 像玩家一样捡起碰到的掉落物，放进背包（PlayerEngine 自己不做这件事）。 */
    private void pickUpItems() {
        List<ItemEntity> items = level().getEntitiesOfClass(ItemEntity.class, getBoundingBox().inflate(1.0, 0.5, 1.0));
        for (ItemEntity item : items) {
            if (item.isRemoved() || item.hasPickUpDelay() || !mayPickUp(item)) {
                continue;
            }
            ItemStack stack = item.getItem();
            int before = stack.getCount();
            ItemStack rest = stack.copy();
            if (!inventory.insertStack(rest)) {
                continue;
            }
            int taken = before - rest.getCount();
            if (taken <= 0) {
                continue;
            }
            take(item, taken);
            if (rest.isEmpty()) {
                item.discard();
            } else {
                item.setItem(rest);
            }
        }
    }

    /**
     * 近战伤害。PlayerEngine 打怪时调 doHurtTarget，而 LivingEntity 本身的 doHurtTarget 什么都不做（只有 Mob 才会造成伤害），
     * 所以照 1.20.1 的 Mob 的做法算：攻击力属性（含手上武器）+ 附魔，击退，火焰附加，附魔的攻击后效果。伤害来源是伙伴，打死的生物算它的。
     */
    @Override
    public boolean doHurtTarget(Entity target) {
        // 玩家、伙伴、有主人的、起了名字的、拴着的、attack_deny 里的都打不伤（正在打伙伴的敌对生物除外，见 AttackTask.mayHit）
        if (!AttackTask.mayHit(this, target)) {
            resetAttackCooldown();
            return false;
        }
        float damage = (float) getAttributeValue(Attributes.ATTACK_DAMAGE);
        float knockback = getAttributes().hasAttribute(Attributes.ATTACK_KNOCKBACK)
                ? (float) getAttributeValue(Attributes.ATTACK_KNOCKBACK) : 0.0F;
        if (target instanceof LivingEntity living) {
            damage += EnchantmentHelper.getDamageBonus(getMainHandItem(), living.getMobType());
            knockback += (float) EnchantmentHelper.getKnockbackBonus(this);
        }
        int fire = EnchantmentHelper.getFireAspect(this);
        if (fire > 0) {
            target.setSecondsOnFire(fire * 4);
        }
        boolean hurt = target.hurt(damageSources().mobAttack(this), damage);
        if (hurt) {
            if (knockback > 0.0F && target instanceof LivingEntity living) {
                living.knockback(knockback * 0.5F, Mth.sin(getYRot() * Mth.DEG_TO_RAD), -Mth.cos(getYRot() * Mth.DEG_TO_RAD));
                setDeltaMovement(getDeltaMovement().multiply(0.6, 1.0, 0.6));
            }
            doEnchantDamageEffects(this, target);
            setLastHurtMob(target);
            playSound(SoundEvents.PLAYER_ATTACK_STRONG, 1.0F, 1.0F);
        }
        resetAttackCooldown();
        return hurt;
    }

    /**
     * 出手后重新算冷却，节奏和玩家一样：攻击速度（空手 4，剑 1.6……）对应 20 / 速度 个 tick。PlayerEngine 在计数到 5 时出手，
     * 所以从 5 - 冷却 开始数。
     */
    private void resetAttackCooldown() {
        double speed = getAttributes().hasAttribute(Attributes.ATTACK_SPEED) ? getAttributeValue(Attributes.ATTACK_SPEED) : 4.0;
        int delay = (int) Math.round(20.0 / Math.max(0.5, speed));
        attackStrengthTicker = Math.min(0, 5 - delay);
    }

    /** 玩家打不伤伙伴（直接打、射箭都不行）：免得 PlayerEngine 的「被玩家打了就还手」去打玩家。 */
    @Override
    public boolean hurt(DamageSource source, float amount) {
        if (source.getEntity() instanceof Player || source.getDirectEntity() instanceof Player) {
            return false;
        }
        return super.hurt(source, amount);
    }

    // ---- 装备直接读写背包（和原版 Player 一样），PlayerEngine 选工具时只改 selectedSlot ----

    @Override
    public Iterable<ItemStack> getArmorSlots() {
        return inventory.armor;
    }

    @Override
    public Iterable<ItemStack> getHandSlots() {
        return List.of(getMainHandItem(), getOffhandItem());
    }

    @Override
    public ItemStack getItemBySlot(EquipmentSlot slot) {
        switch (slot.getType()) {
            case HAND:
                return slot == EquipmentSlot.MAINHAND ? inventory.getMainHandStack() : inventory.offHand.get(0);
            case ARMOR:
                return inventory.armor.get(slot.getIndex());
            default:
                return ItemStack.EMPTY;
        }
    }

    @Override
    public void setItemSlot(EquipmentSlot slot, ItemStack stack) {
        ItemStack old;
        switch (slot.getType()) {
            case HAND:
                if (slot == EquipmentSlot.MAINHAND) {
                    int sel = inventory.selectedSlot;
                    if (!LivingEntityInventory.isValidHotbarIndex(sel)) {
                        return;
                    }
                    old = inventory.main.set(sel, stack);
                } else {
                    old = inventory.offHand.set(0, stack);
                }
                break;
            case ARMOR:
                old = inventory.armor.set(slot.getIndex(), stack);
                break;
            default:
                return;
        }
        onEquipItem(slot, old, stack);
    }

    // ---- 存档 ----

    @Override
    public void addAdditionalSaveData(CompoundTag tag) {
        super.addAdditionalSaveData(tag);
        tag.put("Inventory", inventory.writeNbt(new ListTag()));
        tag.putInt("SelectedItemSlot", inventory.selectedSlot);
        CompoundTag md = new CompoundTag();
        md.putInt("Id", companionId);
        if (owner != null) {
            md.putString("Owner", owner);
        }
        if (ownerUuid != null) {
            md.putString("OwnerUuid", ownerUuid);
        }
        if (this.tag != null) {
            md.putString("Tag", this.tag);
        }
        tag.put("MineDeck", md);
    }

    @Override
    public void readAdditionalSaveData(CompoundTag tag) {
        super.readAdditionalSaveData(tag);
        inventory.readNbt(tag.getList("Inventory", Tag.TAG_COMPOUND));
        inventory.selectedSlot = tag.getInt("SelectedItemSlot");
        CompoundTag md = tag.getCompound("MineDeck");
        companionId = md.getInt("Id");
        owner = md.contains("Owner") ? md.getString("Owner") : null;
        ownerUuid = md.contains("OwnerUuid") ? md.getString("OwnerUuid") : null;
        this.tag = md.contains("Tag") ? md.getString("Tag") : null;
    }

    // ---- 死亡和移除 ----

    @Override
    protected void dropEquipment() {
        inventory.dropAll();
    }

    /** despawn 带 drop:true：背包里的东西掉在原地。 */
    void dropAllItems() {
        inventory.dropAll();
    }

    @Override
    public void die(DamageSource source) {
        super.die(source);
        if (!level().isClientSide) {
            CompanionManager.onDied(this, source);
        }
    }

    @Override
    public void remove(RemovalReason reason) {
        if (!level().isClientSide && (reason.shouldDestroy() || reason == RemovalReason.CHANGED_DIMENSION)) {
            releaseEngine();
        }
        if (!level().isClientSide) {
            CompanionManager.onRemoved(this, reason);
        }
        super.remove(reason);
    }

    /** 停掉 PlayerEngine 的任务，从它的全局表里删掉，并放开它替伙伴强制加载的区块。 */
    private void releaseEngine() {
        if (controller != null) {
            try {
                controller.stop();
            } catch (Throwable t) {
                CompanionManager.LOG.warn("停止 PlayerEngine 控制器时出错：{}", t.toString());
            }
        }
        UUID id = getUUID();
        PlayerEngineController.unregisterFromGlobalRegistry(id);
        ConversationManager.despwnCompanion(id);
        // PlayerEngine 的 ChunkLoadingTracker 用 setChunkForced 强制加载伙伴脚下的 4 个区块，
        // 伙伴没了它也不放开（强制加载会存进存档），这里替它放开
        if (level() instanceof ServerLevel sl) {
            ChunkPos c = chunkPosition();
            for (int dx = -1; dx <= 1; dx++) {
                for (int dz = -1; dz <= 1; dz++) {
                    for (int k = 0; k < 4; k++) {
                        ChunkController.instance.unload(sl, id, c.x + dx, c.z + dz);
                    }
                }
            }
        }
    }
}
