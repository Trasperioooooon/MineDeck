package com.mojang.authlib;

import java.util.UUID;

/** devtest 用的假 GameProfile，只有真类里模组会用到的两个方法。 */
public final class GameProfile {
    private final UUID id;
    private final String name;

    public GameProfile(UUID id, String name) {
        this.id = id;
        this.name = name;
    }

    public UUID getId() {
        return id;
    }

    public String getName() {
        return name;
    }
}
