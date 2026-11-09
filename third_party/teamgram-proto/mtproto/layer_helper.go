package mtproto

// GetClazzNameById returns the predicate a constructor id is registered for,
// "" for an unknown id. pluma: pkg/layercompat reads objects that carry only
// their constructor.
func GetClazzNameById(id int32) string {
	return clazzIdNameRegisters2[id]
}

// IsInLayer says whether a predicate has a constructor in layer. A predicate
// not in the layer tables (the MTProto core, rpc_error and the like) is
// layer-independent and always has one. pluma: pkg/layercompat.
func IsInLayer(predicate string, layer int) bool {
	m, ok := clazzNameRegisters2[predicate]
	if !ok {
		return true
	}
	if _, ok := m[layer]; ok {
		return true
	}
	_, ok = m[0]
	return ok
}
