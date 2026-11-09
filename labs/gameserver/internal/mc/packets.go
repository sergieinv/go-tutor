// Пакет mc — Minecraft-адаптер игрового сервера: приём соединений,
// состояния handshake → status | login → configuration → play,
// кодирование событий мира в пакеты протокола 767 (Minecraft 1.21 / 1.21.1).
//
// Кадрирование и состояния написаны здесь (см. internal/mcproto), из
// go-mc взяты только номера пакетов (data/packetid), таблица состояний
// блоков (level/block) и реестр типов сущностей (data/registryid).
package mc

import (
	"slices"

	"github.com/Tnze/go-mc/data/packetid"
	"github.com/Tnze/go-mc/data/registryid"
)

// Версия протокола и имя версии для списка серверов.
const (
	ProtocolVersion = 767
	VersionName     = "1.21.1"
)

// Номера пакетов берём из go-mc: перечисления там сгенерированы из
// данных игры для 1.21. Тест сверяет ключевые номера с таблицей протокола
// 767 из minecraft.wiki — если go-mc обновится на другую версию, тест
// упадёт, а не клиент.
const (
	// handshake / status / login — номера крошечные и стабильные.
	sbHandshake      = 0x00
	sbStatusRequest  = int32(packetid.ServerboundStatusStatusRequest)
	sbStatusPing     = int32(packetid.ServerboundStatusPingRequest)
	cbStatusResponse = int32(packetid.ClientboundStatusStatusResponse)
	cbStatusPong     = int32(packetid.ClientboundStatusPongResponse)

	sbLoginStart       = int32(packetid.ServerboundLoginHello)
	sbLoginAck         = int32(packetid.ServerboundLoginLoginAcknowledged)
	cbLoginDisconnect  = int32(packetid.ClientboundLoginLoginDisconnect)
	cbLoginSuccess     = int32(packetid.ClientboundLoginGameProfile)
	cbLoginCompression = int32(packetid.ClientboundLoginLoginCompression)

	// configuration
	cbConfigDisconnect   = int32(packetid.ClientboundConfigDisconnect)
	cbConfigFinish       = int32(packetid.ClientboundConfigFinishConfiguration)
	cbConfigKeepAlive    = int32(packetid.ClientboundConfigKeepAlive)
	cbConfigRegistryData = int32(packetid.ClientboundConfigRegistryData)
	cbConfigFeatureFlags = int32(packetid.ClientboundConfigUpdateEnabledFeatures)
	cbConfigKnownPacks   = int32(packetid.ClientboundConfigSelectKnownPacks)
	sbConfigFinish       = int32(packetid.ServerboundConfigFinishConfiguration)
	sbConfigKnownPacks   = int32(packetid.ServerboundConfigSelectKnownPacks)

	// play, clientbound
	cbAddEntity           = int32(packetid.ClientboundAddEntity)
	cbBlockChangedAck     = int32(packetid.ClientboundBlockChangedAck)
	cbBlockUpdate         = int32(packetid.ClientboundBlockUpdate)
	cbChunkBatchFinished  = int32(packetid.ClientboundChunkBatchFinished)
	cbChunkBatchStart     = int32(packetid.ClientboundChunkBatchStart)
	cbDisconnect          = int32(packetid.ClientboundDisconnect)
	cbForgetLevelChunk    = int32(packetid.ClientboundForgetLevelChunk)
	cbGameEvent           = int32(packetid.ClientboundGameEvent)
	cbInitializeBorder    = int32(packetid.ClientboundInitializeBorder)
	cbKeepAlive           = int32(packetid.ClientboundKeepAlive)
	cbLevelChunkWithLight = int32(packetid.ClientboundLevelChunkWithLight)
	cbLogin               = int32(packetid.ClientboundLogin)
	cbMoveEntityPos       = int32(packetid.ClientboundMoveEntityPos)
	cbMoveEntityPosRot    = int32(packetid.ClientboundMoveEntityPosRot)
	cbMoveEntityRot       = int32(packetid.ClientboundMoveEntityRot)
	cbPlayerAbilities     = int32(packetid.ClientboundPlayerAbilities)
	cbPlayerInfoRemove    = int32(packetid.ClientboundPlayerInfoRemove)
	cbPlayerInfoUpdate    = int32(packetid.ClientboundPlayerInfoUpdate)
	cbPlayerPosition      = int32(packetid.ClientboundPlayerPosition)
	cbRemoveEntities      = int32(packetid.ClientboundRemoveEntities)
	cbRotateHead          = int32(packetid.ClientboundRotateHead)
	cbSetChunkCacheCenter = int32(packetid.ClientboundSetChunkCacheCenter)
	cbSetDefaultSpawn     = int32(packetid.ClientboundSetDefaultSpawnPosition)
	cbSetTime             = int32(packetid.ClientboundSetTime)
	cbSystemChat          = int32(packetid.ClientboundSystemChat)
	cbTeleportEntity      = int32(packetid.ClientboundTeleportEntity)

	// play, serverbound
	sbAcceptTeleportation = int32(packetid.ServerboundAcceptTeleportation)
	sbChatCommand         = int32(packetid.ServerboundChatCommand)
	sbChat                = int32(packetid.ServerboundChat)
	sbKeepAlive           = int32(packetid.ServerboundKeepAlive)
	sbMovePlayerPos       = int32(packetid.ServerboundMovePlayerPos)
	sbMovePlayerPosRot    = int32(packetid.ServerboundMovePlayerPosRot)
	sbMovePlayerRot       = int32(packetid.ServerboundMovePlayerRot)
	sbMovePlayerStatus    = int32(packetid.ServerboundMovePlayerStatusOnly)
	sbPlayerAction        = int32(packetid.ServerboundPlayerAction)
	sbSetCarriedItem      = int32(packetid.ServerboundSetCarriedItem)
	sbSetCreativeSlot     = int32(packetid.ServerboundSetCreativeModeSlot)
	sbUseItemOn           = int32(packetid.ServerboundUseItemOn)
)

// playerEntityType — id типа сущности minecraft:player в реестре 1.21 (128).
var playerEntityType = int32(slices.Index(registryid.EntityType, "minecraft:player"))
