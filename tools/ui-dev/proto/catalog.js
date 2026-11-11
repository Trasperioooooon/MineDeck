// 界面原型里「热门清单」用的目录：仿真后端自带的那份除了开头四个，都是「Hot Mod 005」这种占位名字，
// 测试够用，给人看就太假了。原型启动时用这里的条目把占位的换掉（见 ../proto.js 的 dressCatalog）。
// 名字是真实存在的模组和插件（方便看界面在真实长度的名字和说明下是什么样），
// 下载量、许可证、类别是照着印象填的，不保证和 Modrinth 上的一致；原型不联网，也不会真的去下载它们。
"use strict";

// [编号, 名称, 一句说明, 下载量, 许可证, 类别…]
const mod = ([id, title, description, downloads, license, ...cats]) => ({ projectId: id, slug: id, title, description, downloads, license, kind: "mod", cats });
const plugin = ([id, title, description, downloads, license, ...cats]) => ({ projectId: id, slug: id, title, description, downloads, license, kind: "plugin", cats });

const MODS = [
  ["lithium", "Lithium", "No-compromises game logic optimization mod. Well suited for clients and servers of all kinds.", 31204118, "LGPL-3.0", "optimization"],
  ["ferrite-core", "FerriteCore", "Memory usage optimizations.", 29810455, "MIT", "optimization"],
  ["fabric-api", "Fabric API", "Lightweight and modular API providing common hooks and intercompatibility measures.", 27433090, "Apache-2.0", "library"],
  ["geckolib", "GeckoLib", "A 3D animation library for entities, blocks, items, armor, and more.", 21650772, "MIT", "library"],
  ["modernfix", "ModernFix", "All-in-one mod that improves performance, reduces memory usage, and fixes many bugs.", 18220931, "LGPL-3.0", "optimization"],
  ["simple-voice-chat", "Simple Voice Chat", "A working voice chat in Minecraft!", 16904377, "LicenseRef-All-Rights-Reserved", "utility"],
  ["jade", "Jade", "Shows information about what you are looking at.", 14388021, "LicenseRef-CC-BY-NC-SA-4.0", "utility"],
  ["farmers-delight", "Farmer's Delight", "A lightweight farming and cooking expansion, in a rustic style.", 12771640, "MIT", "adventure"],
  ["waystones", "Waystones", "Teleport back to activated waystones. For survival, adventure or servers.", 11932584, "LicenseRef-All-Rights-Reserved", "utility", "adventure"],
  ["curios", "Curios API", "A flexible and expandable accessory and equipment API.", 11105229, "LGPL-3.0", "library", "equipment"],
  ["terralith", "Terralith", "Explore almost 100 new biomes consisting of both realism and fantasy, using just vanilla blocks.", 10647913, "LicenseRef-Stardust-Labs", "worldgen"],
  ["sophisticated-backpacks", "Sophisticated Backpacks", "Fancy and functional backpacks.", 9870442, "GPL-3.0", "storage", "equipment"],
  ["biomes-o-plenty", "Biomes O' Plenty", "Adds over 50 unique biomes to enhance your Minecraft world.", 9412306, "LicenseRef-All-Rights-Reserved", "worldgen"],
  ["ae2", "Applied Energistics 2", "A popular automation and storage mod focused on a matter-energy network.", 8960175, "LGPL-3.0", "technology", "storage"],
  ["spark", "spark", "A performance profiler for Minecraft clients, servers and proxies.", 8733410, "GPL-3.0", "optimization", "utility"],
  ["yungs-better-dungeons", "YUNG's Better Dungeons", "A complete redesign of Minecraft's dungeons.", 8219663, "LGPL-3.0", "adventure", "worldgen"],
  ["mekanism", "Mekanism", "High-tech machinery, energy, gases and tools.", 7905128, "MIT", "technology"],
  ["twilight-forest", "The Twilight Forest", "A realm basked in mystery and eerie twilight; you will overcome terrifying dungeons.", 7641902, "LGPL-2.1", "adventure", "mobs"],
  ["storage-drawers", "Storage Drawers", "Interactive compartment storage for your workshops.", 7312540, "MIT", "storage"],
  ["botania", "Botania", "An innovative natural magic themed tech mod.", 6988071, "LicenseRef-Botania", "magic", "technology"],
  ["alexs-mobs", "Alex's Mobs", "Adds 89 unique creatures to Minecraft.", 6640295, "LGPL-3.0", "mobs"],
  ["c2me", "Concurrent Chunk Management Engine", "A mod designed to improve the chunk performance of Minecraft.", 6312884, "MIT", "optimization"],
  ["tinkers-construct", "Tinkers' Construct", "Modify all the things, then do it again!", 6055317, "MIT", "equipment", "technology"],
  ["ars-nouveau", "Ars Nouveau", "A magic mod inspired by Ars Magicka that allows players to craft custom spells.", 5790426, "LGPL-3.0", "magic"],
  ["when-dungeons-arise", "When Dungeons Arise", "Adds various massive and fun to explore dungeons and structures.", 5514093, "LicenseRef-All-Rights-Reserved", "adventure", "worldgen"],
  ["luckperms-mod", "LuckPerms", "A permissions plugin (mod) for Minecraft servers.", 5236670, "MIT", "management"],
  ["simply-swords", "Simply Swords", "Adds a variety of new weapon types with unique effects.", 4981755, "MIT", "equipment"],
  ["tectonic", "Tectonic", "Terrain generation with massive mountain ranges, underground rivers and more.", 4720398, "MIT", "worldgen"],
  ["irons-spellbooks", "Iron's Spells 'n Spellbooks", "A magic mod that brings back the classic RPG spellcasting fantasy.", 4466012, "LicenseRef-All-Rights-Reserved", "magic"],
  ["carpet", "Carpet", "Take full control over your vanilla game.", 4208346, "MIT", "utility", "management"],
  ["krypton", "Krypton", "A mod to optimize the Minecraft networking stack.", 3950771, "LGPL-3.0", "optimization"],
  ["chunky-mod", "Chunky", "Pre-generates chunks, quickly, efficiently, and safely.", 3712509, "GPL-3.0", "worldgen", "management"],
  ["patchouli", "Patchouli", "Accessible, data-driven, dependency-free documentation for Minecraft modders.", 3489264, "LicenseRef-CC-BY-NC-SA-3.0", "library"],
  ["naturalist", "Naturalist", "Adds a collection of real-world animals with unique behaviours.", 3254187, "LicenseRef-All-Rights-Reserved", "mobs"],
  ["tom-storage", "Tom's Simple Storage Mod", "Simple vanilla style storage mod.", 3021943, "MIT", "storage"],
  ["ledger", "Ledger", "A server-side logging mod: find out who broke what, and roll it back.", 2790658, "LGPL-3.0", "management"],
].map(mod);

const PLUGINS = [
  ["worldedit", "WorldEdit", "A Minecraft map editor that runs in-game.", 9840122, "GPL-3.0", "utility", "worldgen"],
  ["viaversion", "ViaVersion", "Allow newer clients to join older server versions.", 7215904, "GPL-3.0", "utility"],
  ["placeholderapi", "PlaceholderAPI", "The best and simplest way to add placeholders to your server.", 5902376, "GPL-3.0", "library"],
  ["coreprotect", "CoreProtect", "Fast, efficient block logging, rollbacks and restores.", 5377610, "Artistic-2.0", "management"],
  ["worldguard", "WorldGuard", "Protect your server from griefers, and tweak and enable or disable game features.", 4861093, "LGPL-3.0", "management"],
  ["geyser", "Geyser", "A bridge that allows Bedrock Edition players to join Java Edition servers.", 4420587, "MIT", "utility"],
  ["skinsrestorer", "SkinsRestorer", "Restore and change skins on offline-mode servers.", 3984215, "GPL-3.0", "utility"],
  ["voicechat-plugin", "Simple Voice Chat", "A working voice chat in Minecraft!", 3609870, "LicenseRef-All-Rights-Reserved", "social"],
  ["multiverse-core", "Multiverse-Core", "The original Bukkit multi-world management plugin.", 3247133, "BSD-3-Clause", "worldgen", "management"],
  ["bluemap", "BlueMap", "A 3D web map of your Minecraft worlds.", 2905466, "MIT", "utility"],
  ["spark-plugin", "spark", "A performance profiler for Minecraft clients, servers and proxies.", 2688391, "GPL-3.0", "optimization"],
  ["griefprevention", "GriefPrevention", "Stop griefing before it starts: land claims that players manage themselves.", 2390748, "GPL-3.0", "management"],
  ["discordsrv", "DiscordSRV", "The most powerful, configurable, open-source Discord to Minecraft bridging plugin.", 2147062, "GPL-3.0", "social"],
  ["tab", "TAB", "An all-in-one solution for displaying information: tablist, nametags, scoreboard.", 1932514, "Apache-2.0", "social", "utility"],
  ["decentholograms", "DecentHolograms", "A lightweight yet very powerful hologram plugin.", 1705839, "GPL-3.0", "utility"],
  ["quickshop-hikari", "QuickShop-Hikari", "A shop plugin that lets players easily sell and buy items with chests.", 1488207, "GPL-3.0", "economy"],
  ["slimefun", "Slimefun", "A plugin that adds a modpack-like experience, without installing a single mod.", 1296350, "GPL-3.0", "game-mechanics"],
  ["authme", "AuthMeReloaded", "Prevent username stealing on offline-mode servers.", 1104926, "GPL-3.0", "management"],
  ["screaming-bedwars", "Screaming BedWars", "A simple BedWars minigame plugin.", 912477, "LGPL-3.0", "minigame"],
  ["parkour", "Parkour", "Create parkour courses with checkpoints, leaderboards and prizes.", 735018, "MIT", "minigame"],
  ["veinminer", "VeinMiner", "Mine an entire vein of ores at once.", 588243, "GPL-3.0", "game-mechanics"],
].map(plugin);

module.exports = { MODS, PLUGINS };
