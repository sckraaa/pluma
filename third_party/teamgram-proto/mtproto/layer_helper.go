package mtproto

// GetClazzNameById returns the predicate a constructor id is registered for,
// "" for an unknown id. pluma: pkg/layercompat reads objects that carry only
// their constructor.
func GetClazzNameById(id int32) string {
	return clazzIdNameRegisters2[id]
}
