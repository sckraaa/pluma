/*
 * Copyright (c) 2026 The Pluma Authors.
 *  All rights reserved.
 */

package channel

const (
	Predicate_immutableChannel               = "immutableChannel"
	Predicate_immutableChannelParticipant    = "immutableChannelParticipant"
	Predicate_mutableChannel                 = "mutableChannel"
	Predicate_channel_getMutableChannel      = "channel_getMutableChannel"
	Predicate_channel_getChannelListByIdList = "channel_getChannelListByIdList"
)

var clazzNameRegisters2 = map[string]map[int]int32{
	Predicate_immutableChannel: {
		0: -684293246, // 0xd7368382
	},
	Predicate_immutableChannelParticipant: {
		0: 675582471, // 0x28449207
	},
	Predicate_mutableChannel: {
		0: -662027966, // 0xd88a4142
	},
	Predicate_channel_getMutableChannel: {
		0: -437245607, // 0xe5f02959
	},
	Predicate_channel_getChannelListByIdList: {
		0: -1986537988, // 0x8997d5fc
	},
}

var clazzIdNameRegisters2 = map[int32]string{
	-684293246:  Predicate_immutableChannel,               // 0xd7368382
	675582471:   Predicate_immutableChannelParticipant,    // 0x28449207
	-662027966:  Predicate_mutableChannel,                 // 0xd88a4142
	-437245607:  Predicate_channel_getMutableChannel,      // 0xe5f02959
	-1986537988: Predicate_channel_getChannelListByIdList, // 0x8997d5fc
}

func GetClazzID(clazzName string, layer int) int32 {
	if m, ok := clazzNameRegisters2[clazzName]; ok {
		m2, ok2 := m[layer]
		if ok2 {
			return m2
		}
		m2, ok2 = m[0]
		if ok2 {
			return m2
		}
	}
	return 0
}
