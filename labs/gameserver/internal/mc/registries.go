package mc

import "gotutor/labs/gameserver/internal/mcproto"

// Синхронизируемые реестры (configuration → Registry Data).
//
// С 1.20.5 сервер может не присылать содержимое реестров: он объявляет
// «известный набор данных» (known pack) minecraft:core, и клиент с
// ванильными данными берёт содержимое записей у себя. Сервер передаёт
// только имена записей с флагом «данных нет». Поэтому в репозитории нет
// дампов реестров Mojang — только списки идентификаторов.
//
// Списки — ванильные для 1.21.1 (misode/mcmeta, ветка 1.21.1-registries).
// Порядок записей задаёт их числовые id: overworld — 0, plains — 0.
// Некоторые реестры клиент проверяет на полноту: например, все
// damage_type нужны ему при создании мира (DamageSources), а
// painting_variant и wolf_variant не должны быть пустыми. Биомов
// достаточно одного — plains.
var registries = []struct {
	id      string
	entries []string
}{
	{"minecraft:dimension_type", []string{"overworld"}},
	{"minecraft:worldgen/biome", []string{"plains"}},
	{"minecraft:chat_type", []string{
		"chat", "emote_command", "msg_command_incoming", "msg_command_outgoing",
		"say_command", "team_msg_command_incoming", "team_msg_command_outgoing",
	}},
	{"minecraft:damage_type", []string{
		"arrow", "bad_respawn_point", "cactus", "campfire", "cramming", "dragon_breath",
		"drown", "dry_out", "explosion", "fall", "falling_anvil", "falling_block",
		"falling_stalactite", "fireball", "fireworks", "fly_into_wall", "freeze",
		"generic", "generic_kill", "hot_floor", "in_fire", "in_wall", "indirect_magic",
		"lava", "lightning_bolt", "magic", "mob_attack", "mob_attack_no_aggro",
		"mob_projectile", "on_fire", "out_of_world", "outside_border", "player_attack",
		"player_explosion", "sonic_boom", "spit", "stalagmite", "starve", "sting",
		"sweet_berry_bush", "thorns", "thrown", "trident", "unattributed_fireball",
		"wind_charge", "wither", "wither_skull",
	}},
	{"minecraft:trim_pattern", []string{
		"bolt", "coast", "dune", "eye", "flow", "host", "raiser", "rib", "sentry",
		"shaper", "silence", "snout", "spire", "tide", "vex", "ward", "wayfinder", "wild",
	}},
	{"minecraft:trim_material", []string{
		"amethyst", "copper", "diamond", "emerald", "gold", "iron", "lapis",
		"netherite", "quartz", "redstone",
	}},
	{"minecraft:wolf_variant", []string{
		"ashen", "black", "chestnut", "pale", "rusty", "snowy", "spotted", "striped", "woods",
	}},
	{"minecraft:painting_variant", []string{
		"alban", "aztec", "aztec2", "backyard", "baroque", "bomb", "bouquet",
		"burning_skull", "bust", "cavebird", "changing", "cotan", "courbet", "creebet",
		"donkey_kong", "earth", "endboss", "fern", "fighters", "finding", "fire",
		"graham", "humble", "kebab", "lowmist", "match", "meditative", "orb",
		"owlemons", "passage", "pigscene", "plant", "pointer", "pond", "pool",
		"prairie_ride", "sea", "skeleton", "skull_and_roses", "stage", "sunflowers",
		"sunset", "tides", "unpacked", "void", "wanderer", "wasteland", "water",
		"wind", "wither",
	}},
	{"minecraft:banner_pattern", []string{
		"base", "border", "bricks", "circle", "creeper", "cross", "curly_border",
		"diagonal_left", "diagonal_right", "diagonal_up_left", "diagonal_up_right",
		"flow", "flower", "globe", "gradient", "gradient_up", "guster",
		"half_horizontal", "half_horizontal_bottom", "half_vertical",
		"half_vertical_right", "mojang", "piglin", "rhombus", "skull", "small_stripes",
		"square_bottom_left", "square_bottom_right", "square_top_left",
		"square_top_right", "straight_cross", "stripe_bottom", "stripe_center",
		"stripe_downleft", "stripe_downright", "stripe_left", "stripe_middle",
		"stripe_right", "stripe_top", "triangle_bottom", "triangle_top",
		"triangles_bottom", "triangles_top",
	}},
	{"minecraft:enchantment", []string{
		"aqua_affinity", "bane_of_arthropods", "binding_curse", "blast_protection",
		"breach", "channeling", "density", "depth_strider", "efficiency",
		"feather_falling", "fire_aspect", "fire_protection", "flame", "fortune",
		"frost_walker", "impaling", "infinity", "knockback", "looting", "loyalty",
		"luck_of_the_sea", "lure", "mending", "multishot", "piercing", "power",
		"projectile_protection", "protection", "punch", "quick_charge", "respiration",
		"riptide", "sharpness", "silk_touch", "smite", "soul_speed", "sweeping_edge",
		"swift_sneak", "thorns", "unbreaking", "vanishing_curse", "wind_burst",
	}},
	{"minecraft:jukebox_song", []string{
		"11", "13", "5", "blocks", "cat", "chirp", "creator", "creator_music_box",
		"far", "mall", "mellohi", "otherside", "pigstep", "precipice", "relic", "stal",
		"strad", "wait", "ward",
	}},
}

// knownPack — набор данных, который сервер просит клиента взять у себя.
type knownPack struct{ namespace, id, version string }

// Клиенты 1.21 и 1.21.1 объявляют minecraft:core со своей версией.
// Данные реестров у них одинаковые, поэтому предлагаем оба варианта:
// клиент ответит тем, что у него есть.
var serverPacks = []knownPack{
	{"minecraft", "core", "1.21.1"},
	{"minecraft", "core", "1.21"},
}

// registryPackets — заранее собранные тела пакетов Registry Data (без
// кадра): одинаковы для всех клиентов.
func registryPackets() [][]byte {
	out := make([][]byte, 0, len(registries))
	for _, r := range registries {
		b := mcproto.AppendVarInt(nil, cbConfigRegistryData)
		b = mcproto.AppendString(b, r.id)
		b = mcproto.AppendVarInt(b, int32(len(r.entries)))
		for _, e := range r.entries {
			b = mcproto.AppendString(b, "minecraft:"+e)
			b = mcproto.AppendBool(b, false) // данных нет — взять из known pack
		}
		out = append(out, b)
	}
	return out
}
