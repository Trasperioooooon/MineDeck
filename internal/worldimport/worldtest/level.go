package worldtest

import "sort"

// LevelOpts configures synthetic Minecraft Java level.dat generation.
type LevelOpts struct {
	VersionName   string   // "" => omit the Data.Version compound entirely (pre-15w32a world)
	VersionID     int32    // Data.Version.Id; defaults to DataVersion when 0
	DataVersion   int32    // 0 => omit Data.DataVersion
	Snapshot      bool     // Data.Version.Snapshot (byte)
	Series        string   // Data.Version.Series; "" => "main"; only written when Version is written
	LegacyVersion int32    // Data.version (int), e.g. 19133; 0 => omit
	LevelName     string   // Data.LevelName; "" => omit
	ServerBrands  []string // Data.ServerBrands (list of strings); nil => omit
	WasModded     bool     // Data.WasModded (byte); written only when true
	GameType      int32    // Data.GameType
	Difficulty    int8     // Data.Difficulty
	Hardcore      bool     // Data.hardcore (byte)
	AllowCommands bool     // Data.allowCommands (byte)
	Seed          int64    // written as Data.WorldGenSettings.seed when WorldGen is true, else Data.RandomSeed
	WorldGen      bool     // write Data.WorldGenSettings {seed, generate_features, bonus_chest, dimensions{...}}
	CustomDims    []string // extra keys under WorldGenSettings.dimensions (e.g. "mymod:sky")
	GameRules     map[string]string
	EnabledPacks  []string
	DisabledPacks []string
	Player        bool   // write Data.Player (single-player host data, pre-26.1 style)
	PlayerUUID    string // UUID for Data.Player.UUID (int array); default "00000000-0000-0000-0000-00000000000a"

	SingleplayerUUID         string // 26.1+ style: Data.singleplayer_uuid as an int-array UUID; "" => omit
	SingleplayerUUIDAsString bool   // write singleplayer_uuid as a String instead (format variant for tests)

	DataExtra Compound // appended at the end of Data
	RootExtra Compound // appended to the root compound after Data
}

const defaultPlayerUUID = "00000000-0000-0000-0000-00000000000a"

// LevelDat builds the root NBT Compound for a level.dat file.
func LevelDat(o LevelOpts) Compound {
	var data Compound

	if o.VersionName != "" {
		vid := o.VersionID
		if vid == 0 {
			vid = o.DataVersion
		}
		series := o.Series
		if series == "" {
			series = "main"
		}
		data = append(data, Field{
			Name: "Version",
			Value: Compound{
				{Name: "Id", Value: Int(vid)},
				{Name: "Name", Value: String(o.VersionName)},
				{Name: "Series", Value: String(series)},
				{Name: "Snapshot", Value: boolByte(o.Snapshot)},
			},
		})
	}
	if o.DataVersion != 0 {
		data = append(data, Field{Name: "DataVersion", Value: Int(o.DataVersion)})
	}
	if o.LegacyVersion != 0 {
		data = append(data, Field{Name: "version", Value: Int(o.LegacyVersion)})
	}
	if o.LevelName != "" {
		data = append(data, Field{Name: "LevelName", Value: String(o.LevelName)})
	}
	if o.ServerBrands != nil {
		data = append(data, Field{Name: "ServerBrands", Value: stringList(o.ServerBrands)})
	}
	if o.WasModded {
		data = append(data, Field{Name: "WasModded", Value: Byte(1)})
	}

	data = append(data,
		Field{Name: "GameType", Value: Int(o.GameType)},
		Field{Name: "Difficulty", Value: Byte(o.Difficulty)},
		Field{Name: "hardcore", Value: boolByte(o.Hardcore)},
		Field{Name: "allowCommands", Value: boolByte(o.AllowCommands)},
	)

	if o.WorldGen {
		dims := Compound{
			{Name: "minecraft:overworld", Value: Compound{}},
			{Name: "minecraft:the_nether", Value: Compound{}},
			{Name: "minecraft:the_end", Value: Compound{}},
		}
		for _, cd := range o.CustomDims {
			dims = append(dims, Field{Name: cd, Value: Compound{}})
		}
		data = append(data, Field{
			Name: "WorldGenSettings",
			Value: Compound{
				{Name: "seed", Value: Long(o.Seed)},
				{Name: "generate_features", Value: Byte(1)},
				{Name: "bonus_chest", Value: Byte(0)},
				{Name: "dimensions", Value: dims},
			},
		})
	} else {
		data = append(data, Field{Name: "RandomSeed", Value: Long(o.Seed)})
	}

	if o.GameRules != nil {
		keys := make([]string, 0, len(o.GameRules))
		for k := range o.GameRules {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rules := make(Compound, 0, len(keys))
		for _, k := range keys {
			rules = append(rules, Field{Name: k, Value: String(o.GameRules[k])})
		}
		data = append(data, Field{Name: "GameRules", Value: rules})
	}

	if o.EnabledPacks != nil || o.DisabledPacks != nil {
		data = append(data, Field{
			Name: "DataPacks",
			Value: Compound{
				{Name: "Enabled", Value: stringList(o.EnabledPacks)},
				{Name: "Disabled", Value: stringList(o.DisabledPacks)},
			},
		})
	}

	if o.Player {
		puuid := o.PlayerUUID
		if puuid == "" {
			puuid = defaultPlayerUUID
		}
		uuidArr, err := UUIDInts(puuid)
		if err != nil {
			uuidArr = IntArray{0, 0, 0, 0}
		}
		data = append(data, Field{
			Name: "Player",
			Value: Compound{
				{Name: "UUID", Value: uuidArr},
				{Name: "Dimension", Value: String("minecraft:overworld")},
				{Name: "Pos", Value: List{Elem: TagDouble, Items: []any{Double(0.5), Double(64.0), Double(0.5)}}},
				{Name: "XpLevel", Value: Int(30)},
				{Name: "Inventory", Value: List{
					Elem: TagCompound,
					Items: []any{
						Compound{
							{Name: "Slot", Value: Byte(0)},
							{Name: "id", Value: String("minecraft:diamond")},
							{Name: "count", Value: Int(7)},
						},
					},
				}},
				{Name: "EnderItems", Value: List{
					Elem: TagCompound,
					Items: []any{
						Compound{
							{Name: "Slot", Value: Byte(0)},
							{Name: "id", Value: String("minecraft:emerald")},
							{Name: "count", Value: Int(3)},
						},
					},
				}},
			},
		})
	}

	if o.SingleplayerUUID != "" {
		if o.SingleplayerUUIDAsString {
			data = append(data, Field{Name: "singleplayer_uuid", Value: String(o.SingleplayerUUID)})
		} else {
			uuidArr, err := UUIDInts(o.SingleplayerUUID)
			if err != nil {
				uuidArr = IntArray{0, 0, 0, 0}
			}
			data = append(data, Field{Name: "singleplayer_uuid", Value: uuidArr})
		}
	}

	if len(o.DataExtra) > 0 {
		data = append(data, o.DataExtra...)
	}

	root := Compound{{Name: "Data", Value: data}}
	if len(o.RootExtra) > 0 {
		root = append(root, o.RootExtra...)
	}
	return root
}

// WriteLevelDat writes a gzip-compressed level.dat (with empty root name) to path.
func WriteLevelDat(path string, o LevelOpts) error {
	return WriteFile(path, "", LevelDat(o), Gzip)
}

func boolByte(b bool) Byte {
	if b {
		return 1
	}
	return 0
}

func stringList(ss []string) List {
	items := make([]any, len(ss))
	for i, s := range ss {
		items[i] = String(s)
	}
	return List{Elem: TagString, Items: items}
}
