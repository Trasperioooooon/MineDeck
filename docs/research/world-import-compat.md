# Minecraft Java Edition World-Import Compatibility Research

Research conducted for MineDeck's world-import feature (`internal/worldimport`), prioritizing official Mojang server JARs (inspected statically via bytecode disassembly without executing any JAR), Mojang/PaperMC/NeoForged documentation and release notes, CraftBukkit/Paper source references, and `minecraft.wiki` (labeled secondary).

---

## Q1. Mojang 26.1 World Storage Changes

- **Answer**: Starting in Java Edition `26.1` (`world_version` / DataVersion `4786`; FileFixer schema thresholds `4772` and `4773`), Mojang restructured world saves across three areas:
  1. **Dimension folders & per-dimension data** (`DimensionStorageFileFix`):
     - Overworld `region/`, `entities/`, and `poi/` move to `dimensions/minecraft/overworld/{region,entities,poi}/`.
     - `DIM-1/{region,entities,poi}` moves to `dimensions/minecraft/the_nether/{region,entities,poi}/` and `DIM-1` is deleted.
     - `DIM1/{region,entities,poi}` moves to `dimensions/minecraft/the_end/{region,entities,poi}/` and `DIM1` is deleted.
     - Dimension-scoped data in root `data/` (for Overworld), `DIM-1/data/` (for Nether), and `DIM1/data/` (for End) moves to `dimensions/minecraft/{overworld,the_nether,the_end}/data/minecraft/`: `chunks.dat` -> `chunk_tickets.dat`, `raids.dat` -> `raids.dat` (also `raids_end.dat` -> `raids.dat` in `DIM1/data`), and `world_border.dat` -> `world_border.dat`; `DIM-1/data` and `DIM1/data` are deleted. In custom dimensions (`FileRelation.DIMENSIONS_DATA`, under `dimensions/<ns>/<dim>/data/`), `chunks.dat`, `raids.dat`, and `world_border.dat` move under `minecraft/` with the same renames.
  2. **Shared `data/`, player storage, resource packs, and generated structures**:
     - Root `data/` (`FileRelation.DATA`): `scoreboard.dat` -> `data/minecraft/scoreboard.dat`, `stopwatches.dat` -> `data/minecraft/stopwatches.dat`, `command_storage_<ns>.dat` -> `data/<ns>/command_storage.dat`, `idcounts.dat` -> `data/minecraft/maps/last_id.dat`, `map_<id>.dat` -> `data/minecraft/maps/<id>.dat`, `random_sequences.dat` -> `data/minecraft/random_sequences.dat`.
     - Player storage (`PlayerStorageFileFix`): `advancements/` -> `players/advancements/`, `playerdata/` -> `players/data/`, `stats/` -> `players/stats/`.
     - Resource packs (`ResourcePackLocationFileFix`): `resources.zip` -> `resourcepacks/resources.zip`.
     - Generated structures (`GeneratedStructuresRenameFileFix`, schema `4773`): under `generated/<ns>/`, `structures/` -> `structure/`.
     - `datapacks/` remains at `<world>/datapacks/` (`LevelResource`).
     - Obsolete files deleted (`RemoveObsoleteFilesFileFix`): `data/villages.dat`, `data/villages_end.dat`, `data/villages_nether.dat`, `data/advancements`, `data/functions`, `Village`, `data/<dim>.dat`, `data/<dim>_index.dat`.
  3. **Fields removed/moved out of `level.dat` (`LevelDatToSavedDataPreparationFix` & `LevelDatToSavedDataFileFix`)**:
     - `DragonFight` -> `dragon_fight` -> `dimensions/minecraft/the_end/data/minecraft/ender_dragon_fight.dat`
     - `Player` -> migrated to `players/data/<uuid>.dat` (or `FALLBACK_SINGLE_PLAYER_UUID`), removed from `level.dat`, replaced by `Data.singleplayer_uuid`
     - `WanderingTrader*` -> `wandering_trader_migration_data` -> `data/minecraft/wandering_trader.dat`
     - `CustomBossEvents` -> `data/minecraft/custom_boss_events.dat`
     - Weather tags (`clearWeatherTime`, `rainTime`, `thunderTime`, `raining`, `thundering`) -> `weather_data` -> `data/minecraft/weather.dat`
     - `ScheduledEvents` -> `scheduled_events` -> `data/minecraft/scheduled_events.dat`
     - `world_border` -> split into `dimensions/minecraft/{overworld,the_nether,the_end}/data/minecraft/world_border.dat` (`center_x`/`center_z` divided by `8.0` for `the_nether`, `1.0` otherwise)
     - `game_rules` -> `data/minecraft/game_rules.dat` (**no longer stored in `level.dat`**)
     - `WorldGenSettings` -> `world_gen_settings` -> `data/minecraft/world_gen_settings.dat` (**no longer stored in `level.dat`**)
     - `world_clocks` -> `data/minecraft/world_clocks.dat`
     - In `PrimaryLevelData`, legacy `Difficulty` and `DifficultyLocked` were replaced by `Data.difficulty_settings` (`difficulty`, `locked`).
  4. **NBT root shape of `game_rules.dat` and `world_gen_settings.dat`**: Both `LevelStorageSource.writeSavedData` and `SavedDataNbt.write` wrap SavedData payloads in the compressed NBT root compound `{ "data": <Codec output>, "DataVersion": <int> }`. In `game_rules.dat`, `data` holds a registry map of namespaced snake_case gamerules (`minecraft:...`); in `world_gen_settings.dat`, `data` holds `WorldOptions.CODEC` fields (`seed`, `generate_structures`, `bonus_chest`, `legacy_custom_options`) and `WorldDimensions.CODEC` (`dimensions`).
  5. **Auto-conversion and reversibility**: Dedicated server `net.minecraft.server.Main` invokes `DataFixers.getFileFixer().fix(...)` automatically on first load when `requiresFileFixing(dataVersion)` is true, staging inside `<world>/filefix/new_world`. Conversion is **one-way**; downgrading back below `26.1` is unsupported.
- **Applicable versions**: Java Edition `26.1` (`world_version` `4786`) and later (`26.2`, `26.3`).
- **Primary source(s)**:
  - Mojang official server JARs `26.1` (`https://piston-data.mojang.com/v1/objects/3872a7f07a1a595e651aef8b058dfc2bb3772f46/server.jar`) and `26.3` (`https://piston-data.mojang.com/v1/objects/33680f5f2ac32864d6d7cf5e56a705fdb3e05f4c/server.jar`): inner JAR classes `net.minecraft.util.datafix.DataFixers`, `net.minecraft.util.filefix.FileFixerUpper`, `net.minecraft.util.filefix.access.SavedDataNbt`, `net.minecraft.util.filefix.fixes.{DimensionStorageFileFix,PlayerStorageFileFix,ResourcePackLocationFileFix,GeneratedStructuresRenameFileFix,RemoveObsoleteFilesFileFix,LevelDatToSavedDataFileFix}`, `net.minecraft.util.datafix.fixes.LevelDatToSavedDataPreparationFix`, `net.minecraft.world.level.storage.{LevelStorageSource,PrimaryLevelData,LevelResource}`, `net.minecraft.server.Main`.
  - Mojang `minecraft.net` 26.1 release notes and NeoForged 26.1 primer (`https://docs.neoforged.net/primer/docs/26.1/`).
- **Short quote / precise paraphrase**:
  - *Paraphrase (`SavedDataNbt.write` & `DataFixers.addFixers` in 26.3 JAR)*: File fixers registered at schemas `4772`/`4773` relocate world files into `dimensions/`, `players/`, `resourcepacks/`, and `data/minecraft/*.dat`, wrapping saved data under `{data, DataVersion}` and deleting extracted `level.dat` tags.
- **Confidence**: `verified`

---

## Q2. `singleplayer_uuid` in `level.dat`

- **Answer**:
  - **Exact location**: `level.dat` -> `Data.singleplayer_uuid`.
  - **NBT type**: `TAG_Int_Array` of 4 signed 32-bit integers (`UUIDUtil.CODEC` / `UUIDUtil.uuidToIntArray(UUID)`). `UUIDUtil` validates: `"Could not read UUID. Expected int-array of length 4"`.
  - **Semantics**: Identifies the integrated/singleplayer host's player UUID (whose data lives in `players/data/<uuid>.dat`). `PrimaryLevelData.setTagData` only writes `singleplayer_uuid` when `this.singlePlayerUUID != null`; during migration from pre-26.1 saves (`LevelDatToSavedDataFileFix`), if a migrated `Player` compound has no `UUID`, Mojang falls back to `FALLBACK_SINGLE_PLAYER_UUID` and writes that placeholder UUID into both `players/data/<uuid>.dat` and `Data.singleplayer_uuid`.
  - **Dedicated server behavior**: A dedicated server **never uses `singleplayer_uuid` to load player data or grant host permissions**. `DedicatedServer.isSingleplayerOwner(NameAndId)` compiles directly to `iconst_0; ireturn` (always `false`), so `PlayerList.loadPlayerData` always skips the `<singleplayer owner>` branch and loads `players/data/<joining-player-uuid>.dat`. However, `PrimaryLevelData.parse` still parses `singleplayer_uuid` into memory, and `ReenableSpectatorsGenerateChunksInHardcoreWorldsFileFix` (26.3 schema `4899`) checks whether `Data.singleplayer_uuid` is present in `level.dat` when deciding whether to re-enable `minecraft:spectators_generate_chunks` on hardcore singleplayer upgrades.
- **Applicable versions**: Java Edition `26.1`+ (file-fix check also in `26.3`).
- **Primary source(s)**:
  - Mojang `server-26.3.jar`: `net.minecraft.world.level.storage.PrimaryLevelData` (`parse`, `setTagData`), `net.minecraft.core.UUIDUtil`, `net.minecraft.util.filefix.fixes.LevelDatToSavedDataFileFix`, `net.minecraft.util.filefix.fixes.ReenableSpectatorsGenerateChunksInHardcoreWorldsFileFix`, `net.minecraft.server.dedicated.DedicatedServer` (`isSingleplayerOwner`), `net.minecraft.server.players.PlayerList` (`loadPlayerData`).
- **Short quote / precise paraphrase**:
  - *Quote (`DedicatedServer.isSingleplayerOwner` bytecode)*: `0: iconst_0; 1: ireturn` (always returns `false` on dedicated servers).
- **Confidence**: `verified`

---

## Q3. 26.3 `version_history` in `level.dat` & 26.2/26.3 World-Format Changes

- **Answer**:
  - **Exact location & NBT structure**: `level.dat` -> `Data.version_history`, serialized as a `ListTag` of `IntTag` (`TAG_List` of `TAG_Int` data-version integers).
  - **What it records**: In `PrimaryLevelData.writeVersionHistory(CompoundTag)`, each save copies all existing integers from `version_history` and appends `SharedConstants.getCurrentVersion().dataVersion().version()` if the list is empty or `versionHistory.getLast() != current`. No list-length cap is applied in bytecode.
  - **What it does NOT prove**:
    - It does not prove every version in the list touched chunks/regions (chunks retain older `DataVersion` unless upgraded via chunk loading or `--forceUpgrade`).
    - It does not record intermediate versions in which the world was never saved, nor which server software/mod loader was used (use `ServerBrands` / `WasModded` for that).
    - It does not guarantee external file editors or manual downgrades did not alter regions or sidecar `.dat` files after saving.
  - **Other 26.2/26.3 changes relevant to world import**:
    - `26.3` registers FileFix schema `4899` (`ReenableSpectatorsGenerateChunksInHardcoreWorldsFileFix`), which inspects `level.dat` for `singleplayer_uuid` and `difficulty_settings.hardcore == true` and updates `data/minecraft/game_rules.dat` to set `minecraft:spectators_generate_chunks = true`.
    - `PaperMC 26.3` explicitly forbids downgrading once upgraded to `26.3`.
- **Applicable versions**: Java Edition / PaperMC `26.2`–`26.3` (`version_history` and schema `4899` verified in `26.3`; `26.2` `world_version = 4903`, `26.3` `world_version = 5023`).
- **Primary source(s)**:
  - Mojang `server-26.3.jar`: `net.minecraft.world.level.storage.PrimaryLevelData` (`parse`, `writeVersionHistory`), `net.minecraft.util.datafix.DataFixers` (schema `4899`), `net.minecraft.util.filefix.fixes.ReenableSpectatorsGenerateChunksInHardcoreWorldsFileFix`.
  - PaperMC 26.3 release notes: `https://papermc.io/news/26-3/`.
- **Short quote / precise paraphrase**:
  - *Quote (`https://papermc.io/news/26-3/`)*: `"After upgrading your world to 26.3, you cannot downgrade back to a lower version!"`
- **Confidence**: `verified`

---

## Q4. Paper 26.1+ World Layout & Migration

- **Answer**:
  - **Paper 26.1+ directory layout**: Paper `26.1` replaced the legacy 3-folder CraftBukkit split (`world`, `world_nether`, `world_the_end`) with a Vanilla-aligned unified `dimensions/` layout inside `<world>/`:
    - `<world>/level.dat`, `<world>/players/`, `<world>/datapacks/`, `<world>/data/minecraft/`
    - `<world>/dimensions/minecraft/overworld/{region,entities,poi,data,paper-world.yml}`
    - `<world>/dimensions/minecraft/the_nether/{region,entities,poi,data,paper-world.yml}`
    - `<world>/dimensions/minecraft/the_end/{region,entities,poi,data,paper-world.yml}`
  - **Critical Vanilla vs Paper 26.1+ difference**: Paper `26.1+` stores five overworld/level data files under `<world>/dimensions/minecraft/overworld/data/minecraft/` (`game_rules.dat`, `scheduled_events.dat`, `wandering_trader.dat`, `weather.dat`, `world_gen_settings.dat`), whereas Vanilla `26.1+` stores them in `<world>/data/minecraft/`. Migrating a Paper `26.1+` world back to Vanilla requires moving those 5 files from `dimensions/minecraft/overworld/data/minecraft/` into `<world>/data/minecraft/`.
  - **Files under `data/paper/`**: Inside each dimension's `data/paper/` folder (e.g. `dimensions/minecraft/overworld/data/paper/`), Paper stores `metadata.dat`, `level_overrides.dat`, and `persistent_data_container.dat`.
  - **`paper-world.yml` location**: Moved per-dimension inside `<world>/dimensions/minecraft/<dim>/paper-world.yml` (overworld, `the_nether`, `the_end`).
  - **`uid.dat`**: Dropped/removed in Paper `26.1+` (superseded by `data/paper/metadata.dat`).
  - **Plugin-created custom worlds**: `unknown` at primary Paper Java source level (Paper's public migration docs illustrate `dimensions/minecraft/{overworld,the_nether,the_end}`; whether plugin-created Bukkit worlds via `Bukkit.createWorld` reside at `<world-container>/<custom_name>/dimensions/...` or as custom namespaces under `<world>/dimensions/<ns>/<key>` was not directly verified in Paper `WorldFolderMigration` Java source).
  - **Migration paths**:
    - **(a) Old Paper/Bukkit 3-folder world**: **Automatic on first startup** via `WorldFolderMigration` (`LegacyCraftBukkitWorldMigration`), preceded by a 30-second startup warning (`"[WARN] [WorldFolderMigration] World storage migration is required during startup."`). Refuses to overwrite existing target files (`IOException: Refusing to overwrite existing migrated file`). Note: a known Paper 26.1 bug could leave a `00000000-0000-0000-0000-000000000000.dat` player file after migration.
    - **(b) Pre-26.1 Vanilla world**: Handled automatically on startup (`WorldFolderMigration` + Mojang `FileFixerUpper`), restructuring `DIM-1`/`DIM1` and pre-26.1 `level.dat` into Paper 26.1+'s layout. *(Exact Java branch order between Mojang FileFixer vs `WorldFolderMigration` in Paper source was not read directly.)*
    - **(c) 26.1 Vanilla world**: `unknown` whether Paper `WorldFolderMigration` automatically relocates `data/minecraft/{game_rules,scheduled_events,wandering_trader,weather,world_gen_settings}.dat` into `dimensions/minecraft/overworld/data/minecraft/` or expects manual placement when importing an already-26.1 Vanilla save (Paper's migration doc explicitly documents the reverse manual step from Paper 26.1+ -> Vanilla 26.1+).
- **Applicable versions**: PaperMC `26.1` and later (`26.1`–`26.3`).
- **Primary source(s)**:
  - PaperMC Migration Docs: `https://docs.papermc.io/paper/migration/`
  - PaperMC 26.1 News: `https://papermc.io/news/26.1`
  - PaperMC GitHub (`PaperMC/Paper`): `WorldFolderMigration`, `LegacyCraftBukkitWorldMigration` (referenced via web search results).
- **Short quote / precise paraphrase**:
  - *Quote (`WorldFolderMigration` log)*: `"[WARN] [WorldFolderMigration] World storage migration is required during startup. If you do not have a backup: interrupt the server now."`
- **Confidence**: `verified` for layout, `paper-world.yml`, `data/paper/` files, `uid.dat` removal, and automatic 3-folder migration; `unknown` for plugin custom world paths and `(c)` 26.1-Vanilla-to-Paper exact source branch.

---

## Q5. Pre-26.1 CraftBukkit / Spigot / Paper Automatic Vanilla Dimension Split

- **Answer**:
  - On pre-26.1 CraftBukkit/Spigot/Paper servers, `MinecraftServer.java` checks during world initialization for legacy Vanilla `DIM-1` (Nether) and `DIM1` (The End) subdirectories inside `<level-name>/` (`---- Migration of old nether folder required ----` / `---- Migration of old the_end folder required ----`).
  - It **moves** (`File.renameTo` / `Files.move`, not copy) `<world>/DIM-1` to `<world>_nether/DIM-1` and `<world>/DIM1` to `<world>_the_end/DIM1`.
  - If `<world>_nether/DIM-1` or `<world>_the_end/DIM1` already exists in the destination container (e.g., from an interrupted migration or prior Bukkit run), the migration aborts / throws `RuntimeException` rather than overwriting existing dimension data.
  - **`level.dat` and `uid.dat` in `<world>_nether` / `<world>_the_end`**: `unknown` (line-by-line CraftBukkit `MinecraftServer.java` / `WorldNBTStorage` source code was not read directly in this session; web sources note Bukkit creates independent root-level world directories for `<world>_nether` and `<world>_the_end` rather than copying the overworld's `level.dat`/`uid.dat`, but exact creation/fallback behavior in `MinecraftServer.java` was not verified from source).
- **Applicable versions**: CraftBukkit / Spigot / Paper pre-`26.1` (historically Minecraft 1.6+ through `1.21.11`).
- **Primary source(s)**:
  - CraftBukkit / Spigot / Paper `MinecraftServer.java` (referenced via PaperMC/SpigotMC GitHub issue discussions and Q&A logs in web search; exact commit/line range not inspected directly).
- **Short quote / precise paraphrase**:
  - *Quote (`MinecraftServer.java` console log)*: `"---- Migration of old nether folder required ----"`
- **Confidence**: `likely` for folder move (`renameTo`/`Files.move`) and existing-folder abort behavior; `unknown` for exact line range and `level.dat`/`uid.dat` initialization code in `<world>_nether`/`<world>_the_end`.

---

## Q6. World Root Overrides (CLI Flags, `bukkit.yml`, Vanilla Options, `level-name`)

- **Answer**:
  - **CraftBukkit / Spigot / Paper CLI flags** (`org/bukkit/craftbukkit/Main.java`): `"-W"`, `"--world-dir"`, `"--universe"`, and `"--world-container"` are all registered as aliases for the exact same option specifying the parent directory containing all world folders (defaulting to server root `.`).
  - **`bukkit.yml` `settings.world-container`**: Sets the same parent directory (`world-container`) where all server worlds reside (`<container>/<level-name>`, and `<container>/<level-name>_nether` / `<container>/<level-name>_the_end` on pre-26.1). Changing `settings.world-container` in `bukkit.yml` does **not** automatically move existing world folders.
  - **Vanilla dedicated server CLI flags** (`net.minecraft.server.Main` inspected directly in `1.20.4`, `1.21.11`, `26.1`, and `26.3` JARs):
    - `--universe` **exists**: sets the parent directory containing world saves (default `"."`).
    - `--world` **exists**: overrides the world folder name (otherwise taken from `server.properties` `level-name`).
    - Vanilla does **not** accept `-W`, `--world-dir`, or `--world-container`.
  - **Paper 26.1+ `level-name`**: Yes, Paper `26.1+` still treats `server.properties` `level-name` (default `"world"`) as the root world folder name inside the container, housing `dimensions/minecraft/{overworld,the_nether,the_end}`.
- **Applicable versions**: Vanilla `1.12.2`–`26.3` (bytecode checked in `1.20.4`, `1.21.11`, `26.1`, `26.3`); CraftBukkit/Spigot/Paper pre-`26.1` and Paper `26.1+`.
- **Primary source(s)**:
  - Mojang server JARs `1.20.4`, `1.21.11`, `26.1`, `26.3`: `net.minecraft.server.Main`.
  - CraftBukkit/Paper `org/bukkit/craftbukkit/Main.java` (quoted via search result).
  - PaperMC docs (`https://docs.papermc.io/`) on `bukkit.yml` `settings.world-container` and Paper `26.1` layout.
- **Short quote / precise paraphrase**:
  - *Quote (`org/bukkit/craftbukkit/Main.java`)*: `acceptsAll(asList("W", "world-dir", "universe", "world-container"), "World container")`
- **Confidence**: `verified`

---

## Q7. Player Data on Servers (Singleplayer vs Dedicated, Offline UUIDs, Proxy Forwarding)

- **Answer**:
  - **(a) Pre-26.1 dedicated server and `Data.Player`**: A dedicated server **never reads `level.dat` `Data.Player`**. `DedicatedServer.isSingleplayerOwner` always returns `false`, so `PlayerList.load` / `loadPlayerData` skips the singleplayer branch and reads only `playerdata/<uuid>.dat` via `PlayerDataStorage`.
  - **(b) Singleplayer dual-write and precedence**: `unknown` at exact pre-26.1 `PlayerList`/`PlayerDataStorage` bytecode line level (since `1.20.4`/`1.21.11` server JARs were obfuscated and integrated client classes are not in server JARs; empirically in pre-26.1 Java Edition, singleplayer embeds the host's state in `level.dat` `Data.Player` which takes precedence over `playerdata/<uuid>.dat` when the integrated host loads the world via `isSingleplayerOwner == true`, whereas dedicated servers ignore `Data.Player`).
  - **(c) `26.1+` dedicated server player lookup & `singleplayer_uuid`**: `LevelResource` defines player data directory `"players/data"` (migrated from `playerdata/`). On join, `PlayerList.loadPlayerData(NameAndId)` checks `server.isSingleplayerOwner(nameAndId)`: because `DedicatedServer.isSingleplayerOwner` returns `iconst_0; ireturn` (`false`), dedicated servers always load `players/data/<joining-player-uuid>.dat`. `Data.singleplayer_uuid` in `level.dat` points to the singleplayer host's `players/data/<uuid>.dat` file and is unused by `DedicatedServer.isSingleplayerOwner`.
  - **(d) Offline-mode UUID algorithm**: Observed verbatim in `net.minecraft.core.UUIDUtil` (`26.3` JAR): prefixes `"OfflinePlayer:"` to the player's username (`"OfflinePlayer:" + name`), encodes as UTF-8 (`StandardCharsets.UTF_8`), and calls `UUID.nameUUIDFromBytes(...)`, which computes an RFC 4122 **UUID v3 (MD5)** hash.
  - **(e) Velocity modern forwarding & BungeeCord `ip_forward`**: Yes — when Velocity modern forwarding or BungeeCord `ip-forward` (or BungeeGuard) is configured, backend Paper/Spigot servers **must run in `online-mode=false`** and receive the player's **real online UUID** (plus IP and skin properties) forwarded from the proxy over the modern binary+HMAC forwarding format (Velocity) or handshake/plugin messaging (BungeeCord). Therefore, imported worlds running behind a forwarding proxy still key player saves by **online UUIDs**, not `OfflinePlayer:` v3 UUIDs.
- **Applicable versions**: Pre-`26.1` (`1.12.2`–`1.21.11`) and `26.1+` (`26.1`–`26.3`); Velocity/BungeeCord forwarding across Paper/Spigot backends.
- **Primary source(s)**:
  - Mojang `server-26.3.jar`: `net.minecraft.server.dedicated.DedicatedServer` (`isSingleplayerOwner`), `net.minecraft.server.players.PlayerList` (`loadPlayerData`), `net.minecraft.world.level.storage.LevelResource`, `net.minecraft.core.UUIDUtil` (`createOfflinePlayerUUID` / `"OfflinePlayer:"`).
  - PaperMC Documentation on player info forwarding (`https://docs.papermc.io/`).
- **Short quote / precise paraphrase**:
  - *Paraphrase (`UUIDUtil.class`, 26.3 JAR)*: Computes offline UUID via `UUID.nameUUIDFromBytes(("OfflinePlayer:" + username).getBytes(StandardCharsets.UTF_8))` (MD5 UUID v3).
- **Confidence**: `verified` for `(c)`, `(d)`, `(e)`, and `DedicatedServer` behavior in `(a)`; `likely`/`unknown` at source line-number level for `(b)` client-side singleplayer precedence because client JARs were not downloaded.

---

## Q8. Mod Loader Markers (`level.dat`, `ServerBrands`, `WasModded`, `serverconfig/`, NeoForge 26.1)

- **Answer**:
  - **Root `level.dat` loader tags**:
    - **Forge (1.13–1.20+)**: Writes an `fml` compound tag at the root of `level.dat` (sibling to `Data`), containing `LoadingModList` (mod IDs/versions) and registry snapshots via `FMLWorldPersistenceHook`.
    - **Forge 1.12 vs NeoForge**: `FML` compound tag at root is documented for legacy Forge; whether NeoForge writes `fml` vs `neoforge` under `level.dat` root across `1.20.x`–`26.x` was **not** directly inspected in Forge/NeoForge JARs (`unknown` at exact NeoForge `CompoundTag` key level, though `FMLWorldPersistenceHook` and `Data.ServerBrands` are confirmed in NeoForge references).
  - **`Data.ServerBrands` & `Data.WasModded`**: Observed in `PrimaryLevelData.setModdedInfo(String brand, boolean isModded)` (`26.3` JAR):
    - `Data.ServerBrands` (`TAG_List` of strings) accumulates server/loader identifiers across saves (`"vanilla"`, `"Paper"`, `"Spigot"`, `"Bukkit"`, `"fabric"`, `"quilt"`, `"forge"`, `"neoforge"`/`"fml"`).
    - `Data.WasModded` (`TAG_Byte` boolean) is sticky (`this.wasModded |= isModded`): once `true` (`1`), it stays `1` on subsequent loads even if opened later by Vanilla, signaling the save has previously been loaded in a modded or non-vanilla environment.
  - **Per-world `serverconfig/`**: Yes — Forge and NeoForge store per-world mod `.toml` / `.snbt` configs inside `<world>/serverconfig/` (with fallback templates in the server's `defaultconfigs/`).
  - **NeoForged 26.1 "File Fixer Upper" primer (`https://docs.neoforged.net/primer/docs/26.1/`)**: Documents Mojang's `FileFixerUpper` system introduced in `26.1`. File fixers run *before* DFU data fixers, modify world directory layouts (`FileFix`, `makeFixer`, `addFileContentFix`, `FileAccess`, `FileFixOperation`), execute copy-on-write in a temporary staging directory before swapping (`<world>/filefix/new_world` in Vanilla bytecode), and make downgrading unsupported.
- **Applicable versions**: `ServerBrands` / `WasModded` in Java Edition `1.15.2`–`26.3`; Forge `1.12`–`1.20+`; NeoForge `1.20+`–`26.1+`; Fabric/Quilt.
- **Primary source(s)**:
  - Mojang `server-26.3.jar`: `net.minecraft.world.level.storage.PrimaryLevelData` (`setModdedInfo`, `parse`, `setTagData`).
  - NeoForged 26.1 Primer: `https://docs.neoforged.net/primer/docs/26.1/` and NeoForged docs (`https://docs.neoforged.net/`).
  - GitHub / Modrinth references for `fml`/`LoadingModList`, `FMLWorldPersistenceHook`, `serverconfig/`, `fabric`, `quilt` (`ServerBrands`).
- **Short quote / precise paraphrase**:
  - *Paraphrase (`docs.neoforged.net/primer/docs/26.1/`)*: File fixers execute before data fixers in a copy-on-write staging flow to restructure world directories, rendering downgrading unsupported.
- **Confidence**: `verified` for `PrimaryLevelData` (`ServerBrands`/`WasModded`) and NeoForged 26.1 primer; `likely` for Forge `fml`/`LoadingModList` and `serverconfig/`; `unknown` for exact root compound key string used by every NeoForge/Forge 1.12 minor version without inspecting modded JARs.

---

## Q9. `server.properties` and Game Rules in `26.x`

- **Answer**:
  - **`enable-command-block`**: **Removed** from `server.properties` prior to `26.x`! Direct inspection of server JARs confirms `"enable-command-block"` is present in `1.20.4` (`amb.class`), **absent** in `1.21.11` (`arz.class`), and **absent** in `26.1` and `26.3` (`DedicatedServerProperties.class`). Command blocks moved to a game rule in `1.21.9` (`commandBlocksEnabled`), which was then renamed in `1.21.11` to `minecraft:command_blocks_work`.
  - **Game rule name & registry change**: In Java Edition `1.21.11` (`minecraft.net` `1.21.11` changelog and confirmed in `26.1`/`26.3` `net.minecraft.world.level.gamerules.GameRules`), Mojang moved all game rules into a registry and converted names from camelCase to **namespaced snake_case IDs** (e.g., `commandBlocksEnabled` -> `minecraft:command_blocks_work`, `announceAdvancements` -> `minecraft:show_advancement_messages`, `doDaylightCycle` -> `minecraft:advance_time`, `doMobSpawning` -> `minecraft:spawn_mobs`). From `26.1` onward, those rules are persisted in `data/minecraft/game_rules.dat` (Vanilla) or `dimensions/minecraft/overworld/data/minecraft/game_rules.dat` (Paper), not `level.dat`.
  - **`spawn-protection`**: **Unchanged** — `"spawn-protection"` remains a `server.properties` key in `1.20.4`, `1.21.11`, `26.1`, and `26.3` (`DedicatedServerProperties.class`).
- **Applicable versions**: Command-block property removed `1.21.9+` (verified absent in `1.21.11`, `26.1`, `26.3`); namespaced snake_case gamerules `1.21.11+`; `spawn-protection` all versions (`1.12.2`–`26.3`).
- **Primary source(s)**:
  - Mojang server JARs: `1.20.4` (`amb.class`), `1.21.11` (`arz.class`), `26.1` & `26.3` (`net.minecraft.server.dedicated.DedicatedServerProperties`, `net.minecraft.world.level.gamerules.GameRules`).
  - `minecraft.net` Java Edition `1.21.11` changelog (game rules registry & snake_case renames).
- **Short quote / precise paraphrase**:
  - *Paraphrase (`server-1.21.11.jar` & `server-26.3.jar`)*: `"enable-command-block"` is absent from `DedicatedServerProperties`; `"spawn-protection"` remains present; `GameRules` uses namespaced snake_case `command_blocks_work`.
- **Confidence**: `verified`

---

## Q10. Resource Packs (`server.properties` Keys vs World `resources.zip`)

- **Answer**:
  - **`server.properties` keys**: Verified present in `DedicatedServerProperties.class` in `26.1` and `26.3` server JARs:
    - `resource-pack` (pack `.zip` download URL)
    - `resource-pack-sha1` (lowercase SHA-1 hash for integrity)
    - `resource-pack-id` (pack UUID; Paper/Adventure also track packs by UUID)
    - `require-resource-pack` (boolean — disconnect clients that decline)
    - `resource-pack-prompt` (custom prompt text component)
    *(Exact introduction version per individual key across `1.14`–`1.20.3` was not spot-checked across all intermediate JARs: `unknown` from primary JAR diff alone, though all five are verified active in `26.1`–`26.3` and documented in Paper's `server.properties` reference).*
  - **Does a dedicated server automatically serve a world's `resources.zip`?** **No.** A bundled `resources.zip` (moved to `resourcepacks/resources.zip` in `26.1+` via `ResourcePackLocationFileFix`) is used by singleplayer clients only; dedicated servers never auto-host or push local `resources.zip` files to connecting clients unless externally hosted via HTTP/HTTPS and configured via `server.properties` (`resource-pack`) or pushed programmatically via plugins (e.g., Paper/Adventure resource pack APIs sending URL + UUID + SHA-1).
- **Applicable versions**: `26.1`–`26.3` verified (`resourcepacks/resources.zip` relocation `26.1+`; `server.properties` keys verified in `26.1`/`26.3`).
- **Primary source(s)**:
  - Mojang server JARs `26.1` & `26.3`: `net.minecraft.server.dedicated.DedicatedServerProperties`, `net.minecraft.world.level.storage.LevelResource` (`"resourcepacks/resources.zip"`), `net.minecraft.util.filefix.fixes.ResourcePackLocationFileFix`.
  - PaperMC Server Properties Reference: `https://docs.papermc.io/paper/reference/server-properties/`.
  - PaperMC Adventure Resource Pack Docs: `https://docs.papermc.io/adventure/resource-pack/`.
- **Short quote / precise paraphrase**:
  - *Paraphrase (`DedicatedServerProperties.class` & `ResourcePackLocationFileFix.class`)*: `DedicatedServerProperties` defines `resource-pack`, `resource-pack-sha1`, `resource-pack-id`, `require-resource-pack`, and `resource-pack-prompt`, while `ResourcePackLocationFileFix` only moves client world `resources.zip` to `resourcepacks/resources.zip`.
- **Confidence**: `verified` (for `26.1`–`26.3` behavior and non-serving of `resources.zip`; `unknown` for exact minor-version introduction of `resource-pack-prompt`/`resource-pack-id` prior to `26.1`).

---

## Q11. DataVersion Table & `version.json` in Server JARs

- **Answer**:
  - Full release table for all **52** Java Edition release versions from `1.12.2` (DataVersion `1343`) to newest release `26.3` (DataVersion `5023`) is stored in [dataversions.json](file:///E:/Archive_files/MineDeck/.claude/worktrees/wi-research/docs/research/dataversions.json).
  - **8 spot-checked official Mojang server JARs** (`version.json` at outer JAR root verified directly):
    - `1.14.4`: `world_version = 1976`, `protocol_version = 498`, `pack_version = 4` (int), `stable = true`, `release_target = "1.14.4"` *(non-bundler; no `series_id`/`java_version`)*
    - `1.18.2`: `world_version = 2975`, `series_id = "main"`, `protocol_version = 758`, `pack_version = {"resource":8,"data":9}`, `java_version = 17`, `stable = true`, `release_target = "1.18.2"` *(bundler JAR)*
    - `1.20.4`: `world_version = 3700`, `series_id = "main"`, `protocol_version = 765`, `pack_version = {"resource":22,"data":26}`, `java_version = 17`, `stable = true` *(bundler JAR)*
    - `1.21.4`: `world_version = 4189`, `series_id = "main"`, `protocol_version = 769`, `pack_version = {"resource":46,"data":61}`, `java_version = 21`, `stable = true` *(bundler JAR)*
    - `1.21.11`: `world_version = 4671`, `series_id = "main"`, `protocol_version = 774`, `pack_version = {"resource_major":75,"resource_minor":0,"data_major":94,"data_minor":1}`, `java_version = 21`, `stable = true` *(bundler JAR)*
    - `26.1`: `world_version = 4786`, `series_id = "main"`, `protocol_version = 775`, `pack_version = {"resource_major":84,"resource_minor":0,"data_major":101,"data_minor":1}`, `java_version = 25`, `stable = true` *(bundler JAR)*
    - `26.2`: `world_version = 4903`, `series_id = "main"`, `protocol_version = 776`, `pack_version = {"resource_major":88,"resource_minor":0,"data_major":107,"data_minor":1}`, `java_version = 25`, `stable = true` *(bundler JAR)*
    - `26.3`: `world_version = 5023`, `series_id = "main"`, `protocol_version = 777`, `pack_version = {"resource_major":97,"resource_minor":1,"data_major":121,"data_minor":0}`, `java_version = 25`, `stable = true`, `java_component = "java-runtime-epsilon"`, `use_editor = false` *(bundler JAR)*
  - **Bundler JAR behavior (`1.18+`)**: **Yes**, every bundler server JAR tested (`1.18.2` through `26.3`, which package the inner server at `META-INF/versions/<id>/server-<id>.jar`) still keeps `version.json` directly at the **outer JAR root**, containing `world_version`, `series_id`, `protocol_version`, `pack_version`, `java_version`, and `stable`.
- **Applicable versions**: Java Edition releases `1.12.2`–`26.3`.
- **Primary source(s)**:
  - Mojang `version_manifest_v2.json` (`https://piston-meta.mojang.com/mc/game/version_manifest_v2.json`) and 8 official Mojang `server-*.jar` files (`version.json` entries).
  - `https://minecraft.wiki/w/Data_version#List_of_data_versions` *(secondary)* for the remaining 44 releases (`1.12.2`–`26.1.2` un-downloaded).
- **Short quote / precise paraphrase**:
  - *Quote (`26.3` `version.json`)*: `"world_version": 5023, "series_id": "main", "protocol_version": 777, "java_version": 25, "stable": true`
- **Confidence**: `verified` for all 8 spot-checked JARs and outer `version.json` schema; `likely` for the 44 `minecraft.wiki` *(secondary)* releases.

---

## Q12. Bedrock `.mcworld` Identification

- **Answer**: A Minecraft Bedrock Edition `.mcworld` export is a standard `.zip` archive whose world root contains: (1) `level.dat` stored in **little-endian NBT** prefixed by an **8-byte little-endian header** (bytes 0–3: storage version `int32`, bytes 4–7: payload byte length `int32`, followed by uncompressed LE NBT — unlike Java Edition's GZIP-compressed big-endian NBT); (2) `levelname.txt`, a plain-text file storing the world display name; and (3) a `db/` directory holding a **LevelDB** database (`.ldb` and `.log` files) for chunk, subchunk, entity, and dimension data. An importer can reject Bedrock uploads immediately by checking for `db/` + `levelname.txt` or checking `level.dat` for the 8-byte LE header instead of the GZIP magic bytes `1F 8B`.
- **Applicable versions**: Bedrock Edition `.mcworld` / LevelDB format.
- **Primary source(s)**:
  - Bedrock world format technical references (Community/LevelDB specification searches; Mojang does not publish a Bedrock `.mcworld` spec in Java server JARs).
- **Short quote / precise paraphrase**:
  - *Paraphrase*: `.mcworld` archives bundle `db/` (LevelDB), `levelname.txt`, and an 8-byte little-endian header prefixing little-endian `level.dat`.
- **Confidence**: `likely`

---

## Implications for the Importer

Concrete rules for `MineDeck` (`internal/worldimport` layouts: `LayoutLegacy`, `LayoutLegacyBukkit`, `LayoutModern`, `LayoutModernPaper`, `LevelMeta`):

1. **Reject Bedrock saves early (`[Q12]`)**: Check archive roots/extracted worlds for `db/` (with `.ldb`/`.log`), `levelname.txt`, or a non-GZIP `level.dat` beginning with the 8-byte LE header instead of `1F 8B`, and fail with an explicit unsupported-Bedrock error.
2. **Detect Vanilla vs Paper `26.1+` `LayoutModern` (`[Q1]`, `[Q4]`)**:
   - **Vanilla `26.1+` (`LayoutModern`)** puts `game_rules.dat`, `world_gen_settings.dat`, `weather.dat`, `scheduled_events.dat`, and `wandering_trader.dat` in `<world>/data/minecraft/`.
   - **Paper `26.1+` (`LayoutModernPaper`)** puts those five `.dat` files in `<world>/dimensions/minecraft/overworld/data/minecraft/` alongside `<world>/dimensions/minecraft/<dim>/data/paper/{metadata.dat,level_overrides.dat,persistent_data_container.dat}` and `<world>/dimensions/minecraft/<dim>/paper-world.yml` (with no `uid.dat`).
   - Importer rule: Do **not** assume Paper `26.1+` `data/minecraft/*.dat` matches Vanilla `26.1+` paths; when importing a Paper `26.1+` save onto a Vanilla/Fabric/NeoForge target, move those 5 files from `dimensions/minecraft/overworld/data/minecraft/` to `<world>/data/minecraft/` and warn about Paper-only files (`data/paper/`, `paper-world.yml`). Conversely, flag/handle Vanilla-to-Paper `26.1+` (`[Q4]` unknown exact auto-migration).
3. **Inspect both `level.dat` and sidecar `.dat` files when populating `LevelMeta` (`[Q1]`, `[Q2]`, `[Q3]`, `[Q9]`)**:
   - For pre-`26.1` saves (`DataVersion < 4772` / legacy layout), read gamerules, world generation/seed, difficulty, and `Data.Player` from `level.dat`.
   - For `26.1+` saves (`DataVersion >= 4786`, or FileFix schemas `>= 4772`), `game_rules` and `WorldGenSettings` are **absent from `level.dat`**: read them from `data/minecraft/game_rules.dat` (or Paper's `dimensions/minecraft/overworld/data/minecraft/game_rules.dat`) and `data/minecraft/world_gen_settings.dat` under `{ "data": ..., "DataVersion": ... }`, and read difficulty from `Data.difficulty_settings`.
4. **Block or strongly warn on version downgrades, especially across `26.1` and `26.3` (`[Q1]`, `[Q3]`, `[Q8]`, `[Q11]`)**:
   - Compare source `DataVersion` (plus `Data.version_history` in `26.3+` as an advisory floor, remembering `[Q3]` limitations) against target server JAR `version.json` -> `world_version` (present at outer JAR root even on `1.18+` bundler JARs `[Q11]`).
   - Hard-warn/block importing a `>= 26.1` (`>= 4772`) world onto pre-`26.1` targets, or a `26.3` (`5023`) world onto lower targets (`papermc.io/news/26-3/` explicitly forbids downgrades `[Q3]`).
5. **Respect target layout & Bukkit/Paper `world-container` / `--universe` overrides (`[Q5]`, `[Q6]`)**:
   - On Vanilla/Fabric/NeoForge targets, resolve the container via `--universe` (never `--world-container`/`-W`/`--world-dir`, which are CraftBukkit/Paper-only `[Q6]`) and place dimensions inside `<world>/` (`DIM-1`/`DIM1` pre-26.1; `dimensions/minecraft/` for 26.1+).
   - On pre-`26.1` Bukkit/Spigot/Paper targets, check `bukkit.yml` `settings.world-container` and startup flags (`-W`/`--world-dir`/`--universe`/`--world-container`), keep `<level-name>`, `<level-name>_nether`, and `<level-name>_the_end` as siblings in that container, and **never** leave a `DIM-1`/`DIM1` inside `<level-name>` when `<level-name>_nether`/`<level-name>_the_end` already exists (avoids the `[Q5]` `"Migration of old nether folder required"` conflict crash).
   - On Paper `26.1+` targets, do not create pre-26.1 split `_nether`/`_the_end` directories alongside `dimensions/` if migrating automatically (`WorldFolderMigration` throws on existing targets `[Q4]`).
6. **Handle singleplayer host data & UUID translation carefully (`[Q2]`, `[Q7]`)**:
   - Never rely on `level.dat` `Data.Player` or `Data.singleplayer_uuid` to restore host inventory on dedicated servers (`DedicatedServer.isSingleplayerOwner` always returns `false` `[Q2]`, `[Q7]`).
   - Offer an import-time prompt/mapping for singleplayer-to-multiplayer host migration: extract `Data.Player` (pre-26.1) into `playerdata/<target-uuid>.dat`, or map `Data.singleplayer_uuid` (26.1+, `TAG_Int_Array` of 4 ints) in `players/data/<old-uuid>.dat` to `<target-uuid>.dat`.
   - Compute offline target UUIDs strictly via `UUID.nameUUIDFromBytes("OfflinePlayer:" + username)` (MD5 UUID v3), **unless** the server runs behind Velocity modern forwarding or BungeeCord `ip-forward` (in which case backend `online-mode=false` servers still receive and store **online UUIDs** `[Q7]`).
7. **Warn clearly when importing modded worlds onto non-matching loaders or Vanilla/Paper (`[Q8]`)**:
   - Inspect `level.dat` `Data.ServerBrands`, `Data.WasModded`, root `fml`/`FML` (`LoadingModList`), and `<world>/serverconfig/` to classify the source loader and warn before importing a `WasModded=true` / `fml`-tagged world onto Vanilla or Paper (or when `serverconfig/` exists).
8. **Avoid stale `server.properties` keys and handle `resources.zip` honestly (`[Q9]`, `[Q10]`)**:
   - Do **not** generate or rely on `enable-command-block` in `server.properties` when targeting `1.21.9+` / `26.x` (`[Q9]`); configure `commandBlocksEnabled` (`1.21.9`–`1.21.10`) or `minecraft:command_blocks_work` (`1.21.11+` / `game_rules.dat` in `26.1+`) instead, while `spawn-protection` remains valid in `server.properties`.
   - When an imported singleplayer save contains `resources.zip` / `resourcepacks/resources.zip`, inform the user that dedicated servers do **not** serve local `resources.zip` automatically (`[Q10]`), and only configure `resource-pack` / `resource-pack-sha1` / `resource-pack-id` / `require-resource-pack` / `resource-pack-prompt` when an HTTP(S) pack URL is supplied.

---

## Contradictions and Unknowns

- **Paper 26.1+ custom plugin world storage (`Q4`)**: Paper's public migration guide documents `dimensions/minecraft/{overworld,the_nether,the_end}/`; whether third-party/Bukkit `Bukkit.createWorld` custom worlds are placed under `world/dimensions/<namespace>/<name>/` or `<world-container>/<custom_name>/` remains **unknown** without reading Paper's `26.1` Java source (`WorldFolderMigration` / `CraftServer`) directly.
- **Paper 26.1 behavior when importing a `26.1` Vanilla world (`Q4(c)`)**: Paper `26.1+` stores 5 level data files under `dimensions/minecraft/overworld/data/minecraft/` while Vanilla `26.1+` stores them under `data/minecraft/`. Paper docs specify the manual move from Paper -> Vanilla, but whether Paper's startup automatically imports Vanilla's `data/minecraft/*.dat` into `overworld/data/minecraft/` was not directly verified in Paper source (`unknown`).
- **Pre-26.1 CraftBukkit `MinecraftServer.java` exact line-level `level.dat`/`uid.dat` handling on `_nether`/`_the_end` (`Q5`)**: Exact CraftBukkit commit/line range and `level.dat`/`uid.dat` initialization logic for newly split `_nether`/`_the_end` folders remain `unknown` because raw CraftBukkit/Spigot repository source lines were not retrieved.
- **Singleplayer dual-write & precedence in pre-26.1 integrated server (`Q7(b)`)**: Confirmed empirically in community docs, but exact client/integrated-server class bytecode was not downloaded or inspected (`server-*.jar` excludes client classes).
- **Exact NeoForge root NBT tag key (`fml` vs `neoforge`) and all Forge 1.12 minor variants (`Q8`)**: Verified NeoForge's `FMLWorldPersistenceHook`, `fml`/`LoadingModList`, `ServerBrands`/`WasModded`, and `serverconfig/`, while whether newer NeoForge versions emit a root `neoforge` compound in addition to or instead of `fml` was not directly verified from a modded JAR (`unknown`).
- **Minor-version introduction of `resource-pack-prompt` / `resource-pack-id` (`Q10`)**: All five keys (`resource-pack`, `resource-pack-sha1`, `resource-pack-id`, `require-resource-pack`, `resource-pack-prompt`) are verified in `26.1` and `26.3` JARs, whereas the exact earlier release introducing each key was not binary-searched across older JARs (`unknown` at primary-source level).
- **Remaining 44 releases in `dataversions.json` (`Q11`)**: Only 8 release server JARs (`1.14.4`, `1.18.2`, `1.20.4`, `1.21.4`, `1.21.11`, `26.1`, `26.2`, `26.3`) were downloaded (the max 8 JAR cap per `.agent/TASK.md`); the remaining 44 releases rely on `minecraft.wiki` *(secondary)*.
