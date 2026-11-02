package devtest;

import com.mojang.authlib.GameProfile;

/**
 * devtest 用的假 ServerPlayer。Forge 1.20.1 正式环境里 getGameProfile() 的名字是混淆过的 m_36316_，
 * 这里故意用这个名字，验证模组是按返回类型找方法、不是按方法名找。
 */
public final class FakeMcPlayer {
    private final GameProfile profile;

    public FakeMcPlayer(GameProfile profile) {
        this.profile = profile;
    }

    public GameProfile m_36316_() {
        return profile;
    }

    @Override
    public String toString() {
        return "FakeMcPlayer[" + profile.getName() + "]";
    }
}
